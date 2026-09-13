package application

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/felinics/memoh/internal/chat/timeline"
	"github.com/felinics/memoh/internal/models"
)

func validVisionPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestDiscussBadLegacyMediaDoesNotPoisonFollowingImage(t *testing.T) {
	good := validVisionPNG(t)
	reads := 0
	s := &Service{assetLoader: &fakeGatewayAssetLoader{openFn: func(_ context.Context, bot, hash string) (io.ReadCloser, string, error) {
		if bot != "bot-a" {
			t.Fatal("bot scope changed")
		}
		reads++
		data := good
		if hash == "broken" {
			data = []byte("invalid image bytes")
		}
		return io.NopCloser(bytes.NewReader(data)), "image/png", nil
	}}}
	refs := []timeline.ImageAttachmentRef{{ContentHash: "broken", Mime: "image/png"}, {ContentHash: "good", Mime: "image/png"}, {ContentHash: "good", Mime: "image/png"}}
	parts := s.InlineImageAttachments(context.Background(), "bot-a", refs)
	if len(parts) != 1 || reads != 2 {
		t.Fatalf("parts=%d reads=%d", len(parts), reads)
	}
	data, err := base64.StdEncoding.DecodeString(parts[0].Image[len("data:image/png;base64,"):])
	if err != nil || !bytes.Equal(data, good) {
		t.Fatal("valid image was not preserved")
	}
}

func TestStickerGatewayAndInjectionPrepareRasterFrames(t *testing.T) {
	data := validVisionPNG(t)
	s := &Service{assetLoader: &fakeGatewayAssetLoader{openFn: func(context.Context, string, string) (io.ReadCloser, string, error) {
		return io.NopCloser(bytes.NewReader(data)), "application/octet-stream", nil
	}, accessPathFn: func(context.Context, string, string) (string, error) { return "/data/sticker", nil }}}
	req := ChatRequest{BotID: "bot-a", Attachments: []ChatAttachment{{Type: "sticker", ContentHash: "s", Mime: "image/webp"}}}
	got := s.routeAndMergeAttachments(context.Background(), models.GetResponse{Model: models.Model{Config: models.ModelConfig{Compatibilities: []string{models.CompatVision}}}}, req)
	if len(got) != 1 {
		t.Fatalf("got %d attachments", len(got))
	}
	item := got[0].(gatewayAttachment)
	if item.Type != "image" || item.Mime != "image/png" || item.Transport != gatewayTransportInlineDataURL {
		t.Fatalf("invalid prepared frame: %+v", item)
	}
	injected := s.inlineInjectAttachments(context.Background(), "bot-a", req.Attachments)
	if len(injected) != 1 || injected[0].MediaType != "image/png" {
		t.Fatal("injection did not share preparation")
	}
}

func TestMalformedImageFallsBackWithoutResendingOriginalURL(t *testing.T) {
	s := &Service{}
	item := gatewayAttachment{Type: "image", Mime: "image/png", Transport: gatewayTransportInlineDataURL, Payload: "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not PNG")), FallbackPath: "/data/original.png"}
	got := s.prepareVisionAttachments(context.Background(), "bot-a", item)
	if len(got) != 1 || got[0].Type != "file" || got[0].Payload != "/data/original.png" || got[0].Transport != gatewayTransportToolFileRef {
		t.Fatalf("unsafe fallback: %+v", got)
	}
	if parts := extractNativeImageParts(attachmentsToAny(got)); len(parts) != 0 {
		t.Fatal("malformed media still in vision")
	}
}

// Exercise all callers with real gzip/Lottie bytes, including legacy image labels.
func TestAnimatedStickerEntryPoints(t *testing.T) {
	if _, err := exec.LookPath("memoh-sticker-render"); err != nil {
		if os.Getenv("MEMOH_TEST_MEDIA_DECODERS") == "1" {
			t.Fatal(err)
		}
		t.Skip("requires memoh-sticker-render")
	}
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	// A red square travels across a transparent canvas over one second.
	_, err := w.Write([]byte(`{"v":"5.5.2","w":128,"h":64,"ip":0,"op":30,"fr":30,"layers":[{"ty":1,"ind":1,"ip":0,"op":30,"st":0,"sw":16,"sh":16,"sc":"#ff0000","ks":{"o":{"a":0,"k":100},"r":{"a":0,"k":0},"p":{"a":1,"k":[{"t":0,"s":[0,16,0],"e":[100,16,0],"o":{"x":0.33,"y":0.33},"i":{"x":0.67,"y":0.67}},{"t":29,"s":[100,16,0]}]},"a":{"a":0,"k":[0,0,0]},"s":{"a":0,"k":[100,100,100]}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	s := &Service{assetLoader: &fakeGatewayAssetLoader{openFn: func(context.Context, string, string) (io.ReadCloser, string, error) {
		return io.NopCloser(bytes.NewReader(compressed.Bytes())), "application/x-gzip", nil
	}, accessPathFn: func(context.Context, string, string) (string, error) { return "/data/original.tgs", nil }}}
	for _, kind := range []string{"image", "sticker"} {
		req := ChatRequest{BotID: "a", Attachments: []ChatAttachment{{Type: kind, ContentHash: "animation", Mime: "application/x-gzip"}}}
		gateway := s.prepareGatewayAttachments(context.Background(), req)
		if len(gateway) != 5 {
			t.Fatalf("%s gateway frames = %d", kind, len(gateway))
		}
		for _, frame := range gateway {
			if frame.Type != "image" || frame.Mime != "image/png" || frame.FallbackPath != "/data/original.tgs" {
				t.Fatalf("incorrect derived attachment: %+v", frame)
			}
		}
		discuss := s.InlineImageAttachments(context.Background(), "a", []timeline.ImageAttachmentRef{{ContentHash: "animation", Mime: "application/x-gzip", Sticker: kind == "sticker"}})
		injected := s.inlineInjectAttachments(context.Background(), "a", req.Attachments)
		if len(discuss) != 5 || len(injected) != 5 {
			t.Fatalf("%s discuss=%d injected=%d", kind, len(discuss), len(injected))
		}
		for i := range discuss {
			if discuss[i].MediaType != "image/png" || discuss[i].Image != gateway[i].Payload || injected[i].Image != gateway[i].Payload {
				t.Fatal("callers prepared different image frames")
			}
		}
	}
}
