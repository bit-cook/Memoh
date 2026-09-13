package vision

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func imageBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 1024, 256))
	img.SetNRGBA(512, 128, color.NRGBA{R: 255, A: 128})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestStaticStickerUsesBoundedOpaquePNG(t *testing.T) {
	p := NewProcessor()
	frames, err := p.Prepare(context.Background(), "bot-a", imageBytes(t), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || frames[0].MIME != "image/png" {
		t.Fatalf("unexpected frames: %v", len(frames))
	}
	img, _, err := image.Decode(bytes.NewReader(frames[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds().Dx() != 512 || img.Bounds().Dy() != 128 {
		t.Fatal(img.Bounds())
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r != 65535 || g != 65535 || b != 65535 || a != 65535 {
		t.Fatal("transparent pixels were not composited on white")
	}
}

func TestImageValidationAndScopedCache(t *testing.T) {
	p := NewProcessor()
	data := imageBytes(t)
	if _, err := p.Prepare(context.Background(), "a", []byte("not an image"), false); err == nil {
		t.Fatal("invalid image admitted")
	}
	frames, err := p.Prepare(context.Background(), "a", data, false)
	if err != nil {
		t.Fatal(err)
	}
	frames[0].Data[0] = 0
	cached, err := p.Prepare(context.Background(), "a", data, false)
	if err != nil || cached[0].Data[0] != 137 {
		t.Fatal("caller mutated cached bytes")
	}
	if _, err = p.Prepare(context.Background(), "b", data, false); err != nil {
		t.Fatal(err)
	}
	if len(p.cache) != 2 {
		t.Fatal("cache crossed bot scopes")
	}
	if _, err = p.Prepare(context.Background(), "a", make([]byte, MaxBytes+1), true); err == nil {
		t.Fatal("oversized media accepted")
	}
}

func TestFrameIndices(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want []int
	}{{1, []int{0}}, {3, []int{0, 1, 2}}, {30, []int{0, 7, 15, 22, 29}}} {
		if got := frameIndices(tc.n); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("n=%d: %v", tc.n, got)
		}
	}
}

func gzipJSON(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestTGSRejectsUnsafeOrInvalidInputBeforeRenderer(t *testing.T) {
	for _, s := range []string{`{}`, `{"w":512,"h":512,"ip":0,"op":10,"fr":30,"assets":[{"p":"/etc/passwd"}]}`, `{"w":1,"h":1,"ip":0,"op":999999,"fr":30}`, `{"w":1,"h":1,"ip":0,"op":1,"fr":0}`} {
		if _, err := NewProcessor().Prepare(context.Background(), "a", gzipJSON(t, s), false); err == nil {
			t.Fatal("unsafe TGS accepted")
		}
	}
}

func TestDecoderSlotHonorsCancellation(t *testing.T) {
	p := NewProcessor()
	p.slots <- struct{}{}
	p.slots <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Prepare(ctx, "a", imageBytes(t), true); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func requireDecoder(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		if os.Getenv("MEMOH_TEST_MEDIA_DECODERS") == "1" {
			t.Fatalf("required decoder %s missing", name)
		}
		t.Skip("decoder integration requires " + name)
	}
}

func TestVideoStickerIntegration(t *testing.T) {
	requireDecoder(t, "ffmpeg")
	requireDecoder(t, "ffprobe")
	dir := t.TempDir()
	input := filepath.Join(dir, "sample.webm")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=duration=1:size=640x320:rate=12", "-c:v", "libvpx-vp9", "-threads", "1", input) // #nosec G204 -- Fixed command/options and a test-owned temporary path.
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate video: %v: %s", err, out)
	}
	data, err := os.ReadFile(input) // #nosec G304 -- Test-owned temporary media fixture.
	if err != nil {
		t.Fatal(err)
	}
	// Legacy image records have no sticker flag. Magic-byte dispatch must still convert them.
	frames, err := NewProcessor().Prepare(ctx, "bot", data, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 5 {
		t.Fatalf("wanted five distinct frames, got %d", len(frames))
	}
	for _, f := range frames {
		cfg, format, err := image.DecodeConfig(bytes.NewReader(f.Data))
		if err != nil || format != "png" || cfg.Width != 512 || cfg.Height != 256 {
			t.Fatalf("bad frame: %v %s %v", cfg, format, err)
		}
	}
}

func TestTGSIntegration(t *testing.T) {
	requireDecoder(t, "memoh-sticker-render")
	data := gzipJSON(t, `{"v":"5.5.2","w":512,"h":256,"ip":0,"op":60,"fr":30,"layers":[]}`)
	frames, err := NewProcessor().Prepare(context.Background(), "bot", data, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("identical frames should deduplicate, got %d", len(frames))
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(frames[0].Data))
	if err != nil || format != "png" || cfg.Width != 512 || cfg.Height != 256 {
		t.Fatalf("bad TGS frame: %v %s %v", cfg, format, err)
	}
}

func TestTransparentVideoStickerIntegration(t *testing.T) {
	requireDecoder(t, "ffmpeg")
	requireDecoder(t, "ffprobe")
	input := filepath.Join(t.TempDir(), "transparent.webm")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=black@0.0:size=32x32:rate=1:duration=1,format=yuva420p", "-c:v", "libvpx-vp9", "-threads", "1", input) // #nosec G204 -- Fixed command/options and a test-owned temporary path.
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate alpha video: %v: %s", err, out)
	}
	data, err := os.ReadFile(input) // #nosec G304 -- Test-owned temporary media fixture.
	if err != nil {
		t.Fatal(err)
	}
	frames, err := NewProcessor().Prepare(ctx, "bot", data, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 {
		t.Fatalf("frames: %d", len(frames))
	}
	img, _, err := image.Decode(bytes.NewReader(frames[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r != 65535 || g != 65535 || b != 65535 || a != 65535 {
		t.Fatalf("transparent WebM was not composited on white: %d %d %d %d", r, g, b, a)
	}
}

func TestConcurrentPreparationKeepsOneCacheEntry(t *testing.T) {
	p := NewProcessor()
	data := imageBytes(t)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			frames, err := p.Prepare(context.Background(), "bot", data, true)
			if err != nil || len(frames) != 1 {
				t.Errorf("concurrent preparation: frames=%d error=%v", len(frames), err)
			}
		})
	}
	wg.Wait()
	if len(p.cache) != 1 || p.cacheBytes != len(p.cache[0].frames[0].Data) {
		t.Fatal("concurrent inserts duplicated cache entries or byte accounting")
	}
}
