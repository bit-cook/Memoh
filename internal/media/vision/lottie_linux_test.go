//go:build linux && (amd64 || arm64)

package vision

import (
	"image/color"
	"testing"
)

func TestCompositeARGB(t *testing.T) {
	img := compositeARGB([]uint32{0, 0xffff0000, 0x80800000, 0xff123456}, 4, 1)
	want := []color.RGBA{{255, 255, 255, 255}, {255, 0, 0, 255}, {255, 127, 127, 255}, {0x12, 0x34, 0x56, 255}}
	for i, expected := range want {
		if got := img.RGBAAt(i, 0); got != expected {
			t.Errorf("pixel %d = %v, want %v", i, got, expected)
		}
	}
}
