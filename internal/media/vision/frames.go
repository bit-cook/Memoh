// Package vision prepares stored media for image-only model inputs. Originals
// remain in the media store; derived frames never become conversation history.
package vision

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"  // Register GIF detection for animation preparation.
	_ "image/jpeg" // Register JPEG decoding for validation.
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // Register Telegram static sticker decoding.
)

const (
	MaxBytes      = 20 * 1024 * 1024
	MaxFrames     = 5
	MaxEdge       = 512
	maxPixels     = 16 * 1024 * 1024
	maxCacheBytes = 16 * 1024 * 1024
)

// Frame always contains a decoded/validated raster image, never a video or TGS.
type Frame struct {
	Data []byte
	MIME string
}
type cachedFrames struct {
	key    string
	frames []Frame
	size   int
}

// Processor bounds decoder concurrency and keeps a small, process-local cache.
// The caller's scope is included in cache keys to keep tenant media separate.
type Processor struct {
	slots      chan struct{}
	mu         sync.Mutex
	cache      []cachedFrames
	cacheBytes int
}

func NewProcessor() *Processor { return &Processor{slots: make(chan struct{}, 2)} }

func (p *Processor) Prepare(ctx context.Context, scope string, data []byte, sticker bool) ([]Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > MaxBytes {
		return nil, errors.New("media exceeds image preparation size limit or is empty")
	}
	digest := sha256.Sum256(data)
	key := fmt.Sprintf("%s:%t:%x", scope, sticker, digest)
	if frames := p.lookup(key); frames != nil {
		return frames, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if frames := p.lookup(key); frames != nil {
		return frames, nil
	}
	frames, err := prepare(ctx, data, sticker)
	if err != nil {
		return nil, err
	}
	seen := map[[32]byte]bool{}
	unique := make([]Frame, 0, len(frames))
	for _, f := range frames {
		sum := sha256.Sum256(f.Data)
		if !seen[sum] {
			seen[sum] = true
			unique = append(unique, f)
		}
	}
	p.remember(key, unique)
	return unique, nil
}

func (p *Processor) lookup(key string) []Frame {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, c := range p.cache {
		if c.key == key {
			copy(p.cache[i:], p.cache[i+1:])
			p.cache[len(p.cache)-1] = c
			return cloneFrames(c.frames)
		}
	}
	return nil
}

func cloneFrames(in []Frame) []Frame {
	out := make([]Frame, len(in))
	for i, f := range in {
		out[i] = Frame{Data: bytes.Clone(f.Data), MIME: f.MIME}
	}
	return out
}

func (p *Processor) remember(key string, frames []Frame) {
	size := 0
	for _, f := range frames {
		size += len(f.Data)
	}
	if size > maxCacheBytes {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.cache {
		if c.key == key {
			return
		}
	}
	for len(p.cache) > 0 && (p.cacheBytes+size > maxCacheBytes || len(p.cache) >= 64) {
		p.cacheBytes -= p.cache[0].size
		p.cache = p.cache[1:]
	}
	p.cache = append(p.cache, cachedFrames{key, cloneFrames(frames), size})
	p.cacheBytes += size
}

func prepare(ctx context.Context, data []byte, sticker bool) ([]Frame, error) {
	// Trust bytes instead of historical MIME/type labels. TGS is gzipped JSON;
	// Telegram video stickers use the EBML container magic.
	if bytes.HasPrefix(data, []byte{0x1f, 0x8b}) {
		return renderTGS(ctx, data)
	}
	if bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}) || (len(data) >= 8 && string(data[4:8]) == "ftyp") {
		return extractVideo(ctx, data)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("attachment is not a supported raster image, video sticker, or TGS")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, errors.New("image dimensions exceed preparation limit")
	}
	if format == "gif" {
		return extractVideo(ctx, data)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("image pixels cannot be decoded")
	}
	if !sticker {
		return []Frame{{Data: bytes.Clone(data), MIME: "image/" + format}}, nil
	}
	frame, err := rasterFrame(img)
	if err != nil {
		return nil, err
	}
	return []Frame{frame}, nil
}

func rasterFrame(img image.Image) (Frame, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := math.Min(1, math.Min(float64(MaxEdge)/float64(w), float64(MaxEdge)/float64(h)))
	w, h = max(1, int(float64(w)*scale)), max(1, int(float64(h)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return Frame{}, err
	}
	return Frame{Data: buf.Bytes(), MIME: "image/png"}, nil
}

func frameIndices(total int) []int {
	count := min(total, MaxFrames)
	if count <= 0 {
		return nil
	}
	if count == 1 {
		return []int{0}
	}
	out := make([]int, count)
	for i := range out {
		out[i] = int(math.Round(float64(i) * float64(total-1) / float64(count-1)))
	}
	return out
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64*1024 {
		return 0, errors.New("decoder diagnostic limit exceeded")
	}
	return b.Buffer.Write(p)
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	// Executable names and option lists are internal constants; no shell is used.
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204
	cmd.WaitDelay = time.Second
	var output limitedOutput
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s could not prepare sticker frames", filepath.Base(name))
	}
	return output.Bytes(), nil
}

func tempInput(data []byte) (string, string, error) {
	dir, err := os.MkdirTemp("", "memoh-vision-")
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(dir, "input")
	if err = os.WriteFile(path, data, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", "", err
	}
	return dir, path, nil
}

func readFrames(dir string, count int) ([]Frame, error) {
	out := make([]Frame, 0, count)
	for i := 1; i <= count; i++ {
		// Only numbered decoder outputs inside our private temporary directory.
		f, err := os.Open(filepath.Join(dir, fmt.Sprintf("frame_%d.png", i))) // #nosec G304
		if err != nil {
			return nil, errors.New("decoder did not produce the selected frames")
		}
		data, err := io.ReadAll(io.LimitReader(f, 2*1024*1024))
		_ = f.Close()
		if err != nil {
			return nil, err
		}
		cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || format != "png" || cfg.Width > MaxEdge || cfg.Height > MaxEdge {
			return nil, errors.New("decoder produced an invalid frame")
		}
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		frame, err := rasterFrame(img)
		if err != nil {
			return nil, err
		}
		out = append(out, frame)
	}
	return out, nil
}

func extractVideo(ctx context.Context, data []byte) ([]Frame, error) {
	dir, input, err := tempInput(data)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	probeBase := []string{"-v", "error", "-max_alloc", "67108864", "-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,gif"}
	probe := append(append([]string{}, probeBase...), "-select_streams", "v:0", "-show_entries", "stream=width,height,nb_frames,duration,codec_name:format=duration", "-of", "json", input)
	raw, err := run(ctx, "ffprobe", probe...)
	if err != nil {
		return nil, err
	}
	var metadata struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Count    string `json:"nb_frames"`
			Duration string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if json.Unmarshal(raw, &metadata) != nil || len(metadata.Streams) != 1 {
		return nil, errors.New("video stream metadata unavailable")
	}
	stream := metadata.Streams[0]
	if stream.Width <= 0 || stream.Height <= 0 || int64(stream.Width)*int64(stream.Height) > maxPixels {
		return nil, errors.New("video dimensions exceed preparation limit")
	}
	duration, _ := strconv.ParseFloat(stream.Duration, 64)
	containerDuration, _ := strconv.ParseFloat(metadata.Format.Duration, 64)
	if duration > 60 || containerDuration > 60 {
		return nil, errors.New("animation duration exceeds preparation limit")
	}
	total, _ := strconv.Atoi(stream.Count)
	if total <= 0 {
		probe = append(append([]string{}, probeBase...), "-count_frames", "-select_streams", "v:0", "-show_entries", "stream=nb_read_frames", "-of", "default=noprint_wrappers=1:nokey=1", input)
		raw, err = run(ctx, "ffprobe", probe...)
		if err != nil {
			return nil, err
		}
		total, _ = strconv.Atoi(strings.TrimSpace(string(raw)))
	}
	if total <= 0 || total > 7200 {
		return nil, errors.New("animation frame count exceeds preparation limit")
	}
	indices := frameIndices(total)
	terms := make([]string, len(indices))
	for i, n := range indices {
		terms[i] = fmt.Sprintf("eq(n\\,%d)", n)
	}
	filter := "select=" + strings.Join(terms, "+") + ",scale=w='min(512,iw)':h='min(512,ih)':force_original_aspect_ratio=decrease"
	args := []string{"-nostdin", "-v", "error", "-max_alloc", "67108864", "-protocol_whitelist", "file,pipe", "-format_whitelist", "matroska,webm,mov,gif", "-threads", "1"}
	// The native VP8/VP9 decoders discard WebM alpha. libvpx preserves it so
	// transparent sticker backgrounds can be composited on white afterwards.
	switch stream.Codec {
	case "vp9":
		args = append(args, "-c:v", "libvpx-vp9")
	case "vp8":
		args = append(args, "-c:v", "libvpx")
	}
	args = append(args, "-i", input, "-an", "-vf", filter, "-vsync", "vfr", "-frames:v", strconv.Itoa(len(indices)), "-threads", "1", filepath.Join(dir, "frame_%d.png"))
	_, err = run(ctx, "ffmpeg", args...)
	if err != nil {
		return nil, err
	}
	return readFrames(dir, len(indices))
}

func renderTGS(ctx context.Context, data []byte) ([]Frame, error) {
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("invalid compressed sticker")
	}
	raw, err := io.ReadAll(io.LimitReader(r, 2*1024*1024+1))
	_ = r.Close()
	if err != nil || len(raw) > 2*1024*1024 {
		return nil, errors.New("sticker JSON exceeds preparation limit or is corrupt")
	}
	var doc struct {
		W      float64                      `json:"w"`
		H      float64                      `json:"h"`
		IP     float64                      `json:"ip"`
		OP     float64                      `json:"op"`
		FR     float64                      `json:"fr"`
		Assets []map[string]json.RawMessage `json:"assets"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.W <= 0 || doc.H <= 0 || doc.W > 4096 || doc.H > 4096 || doc.FR <= 0 || doc.FR > 120 || doc.OP <= doc.IP || doc.OP-doc.IP > 7200 || (doc.OP-doc.IP)/doc.FR > 60 {
		return nil, errors.New("invalid sticker dimensions or frame range")
	}
	for _, asset := range doc.Assets {
		if _, ok := asset["p"]; ok {
			return nil, errors.New("external or embedded bitmap assets are not supported in animated stickers")
		}
		if _, ok := asset["u"]; ok {
			return nil, errors.New("external assets are not supported in animated stickers")
		}
	}
	dir, input, err := tempInput(raw)
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	output, err := run(ctx, "memoh-sticker-render", input, dir)
	if err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil || count <= 0 || count > MaxFrames {
		return nil, errors.New("sticker renderer returned invalid frame count")
	}
	return readFrames(dir, count)
}
