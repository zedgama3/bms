//go:build integration

package settings_test

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
)

// nameMarker is an invented file name that must reach no log, span or audit entry.
const nameMarker = "Quibble-Frondwhistle-5521"

func jpegOf(t testing.TB) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := range 8 {
		img.Set(x, x, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return b.Bytes()
}

// webpOf is a minimal lossless WebP container (VP8X then VP8L, the shape files' own tests
// use): enough for the sniff and the metadata strip.
func webpOf() []byte {
	chunk := func(id string, data []byte) []byte {
		out := []byte(id)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(data))) //nolint:gosec // bounded fixture sizes
		out = append(out, data...)
		if len(data)%2 == 1 {
			out = append(out, 0)
		}
		return out
	}
	body := []byte("WEBP")
	body = append(body, chunk("VP8X", []byte{0, 0, 0, 0, 7, 0, 0, 7, 0, 0})...)
	body = append(body, chunk("VP8L", []byte{0x2f, 0x07, 0xc0, 0x01, 0x00, 0x10, 0x20, 0x30, 0x40, 0x50})...)
	out := []byte("RIFF")
	out = binary.LittleEndian.AppendUint32(out, uint32(len(body))) //nolint:gosec // bounded fixture sizes
	return append(out, body...)
}

func (s *spyStore) setAttachErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attachErr = err
}

func entriesWith(es []audit.Entry, action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range es {
		if string(e.Action) == action {
			out = append(out, e)
		}
	}
	return out
}
