//go:build linux && (amd64 || arm64)

package vision

import (
	"context"
	"errors"
	"fmt"
	"image"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
)

// These signatures follow rlottie_capi.h: size_t is uintptr on both supported
// platforms. Only synchronous rendering is used; rlottie never retains pixels.
type lottieAPI struct {
	init    func()
	cache   func(uintptr)
	create  func(string, string, string) uintptr
	destroy func(uintptr)
	size    func(uintptr, *uintptr, *uintptr)
	frames  func(uintptr) uintptr
	render  func(uintptr, uintptr, *uint32, uintptr, uintptr, uintptr)
}

// Keep the library initialized for the process lifetime. Model caching is
// disabled because the application owns its own bot-scoped frame cache.
var loadLottie = sync.OnceValues(func() (*lottieAPI, error) {
	lib, err := purego.Dlopen("librlottie.so.0", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("%w: rlottie runtime library could not be loaded", ErrTGSUnavailable)
	}
	closeLibrary := func() { _ = purego.Dlclose(lib) }
	api := &lottieAPI{}
	for _, symbol := range []struct {
		name string
		fn   any
	}{
		{"lottie_init", &api.init},
		{"lottie_configure_model_cache_size", &api.cache},
		{"lottie_animation_from_data", &api.create},
		{"lottie_animation_destroy", &api.destroy},
		{"lottie_animation_get_size", &api.size},
		{"lottie_animation_get_totalframe", &api.frames},
		{"lottie_animation_render", &api.render},
	} {
		address, err := purego.Dlsym(lib, symbol.name)
		if err != nil {
			closeLibrary()
			return nil, fmt.Errorf("%w: rlottie symbol %s", ErrTGSUnavailable, symbol.name)
		}
		purego.RegisterFunc(symbol.fn, address)
	}
	api.init()
	api.cache(0)
	return api, nil
})

func renderLottie(ctx context.Context, data []byte) ([]Frame, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	api, err := loadLottie()
	if err != nil {
		return nil, err
	}
	animation := api.create(string(data), "sticker", "")
	if animation == 0 {
		return nil, errors.New("invalid Lottie animation")
	}
	defer api.destroy(animation)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var width, height uintptr
	api.size(animation, &width, &height)
	total := api.frames(animation)
	if width == 0 || height == 0 || width > 4096 || height > 4096 || total == 0 || total > 7200 {
		return nil, errors.New("invalid animation dimensions or frame count")
	}
	// Integer scaling preserves aspect ratio without allocating the full canvas.
	if edge := max(width, height); edge > MaxEdge {
		width, height = max(1, width*MaxEdge/edge), max(1, height*MaxEdge/edge)
	}
	pixels := make([]uint32, width*height)
	indices := frameIndices(int(total))
	out := make([]Frame, 0, len(indices))
	for _, index := range indices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		clear(pixels)
		// Context cancellation is checked around the synchronous native call;
		// it cannot interrupt rlottie while that call is executing.
		api.render(animation, uintptr(index), &pixels[0], width, height, width*4)
		runtime.KeepAlive(pixels)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		frame, err := rasterFrame(compositeARGB(pixels, int(width), int(height)))
		if err != nil {
			return nil, err
		}
		out = append(out, frame)
	}
	return out, ctx.Err()
}

// rlottie returns premultiplied ARGB words. Composite on white and emit opaque
// Go RGBA pixels, independent of the host byte order.
func compositeARGB(pixels []uint32, width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for i, p := range pixels {
		white := 255 - ((p >> 24) & 255)
		img.Pix[i*4] = uint8(min(255, ((p>>16)&255)+white))
		img.Pix[i*4+1] = uint8(min(255, ((p>>8)&255)+white))
		img.Pix[i*4+2] = uint8(min(255, (p&255)+white))
		img.Pix[i*4+3] = 255
	}
	return img
}
