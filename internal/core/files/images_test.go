//nolint:gosec // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// Fixtures are generated here, never read from disk or taken from a real photo. Every piece of
// "metadata" carries an invented marker so a test can prove it is gone from the stored bytes.
const (
	markGPS    = "GPSLAT-0773"           // inside the Exif GPS IFD
	markMake   = "Quillfeather-Phone-X1" // device make in Exif IFD0
	markDate   = "2026:10:20 12:34:56"   // Exif DateTime
	markText   = "TEXTMARK-0773"         // PNG text chunks, GIF comment, JPEG COM, XMP
	markXMP    = "XMPMARK-0773"
	markPhotos = "PSMARK-0773" // Photoshop APP13
)

// pattern fills a w x h image with a deterministic, non-uniform pattern so a decoder can tell
// whether pixels survived.
func pattern(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 31), G: uint8(y * 29), B: uint8((x + y) * 13), A: 255})
		}
	}
	return img
}

// ---- PNG ----

type pngChunk struct {
	typ  string
	data []byte
}

func plainPNG(t testing.TB) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, pattern(8, 8)); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}
	return b.Bytes()
}

func parsePNG(t testing.TB, b []byte) []pngChunk {
	t.Helper()
	sig := []byte("\x89PNG\r\n\x1a\n")
	if !bytes.HasPrefix(b, sig) {
		t.Fatalf("not a PNG (starts %q)", b[:min(8, len(b))])
	}
	var out []pngChunk
	for i := len(sig); i < len(b); {
		if i+12 > len(b) {
			t.Fatalf("truncated PNG chunk header at %d", i)
		}
		n := int(binary.BigEndian.Uint32(b[i:]))
		if i+12+n > len(b) {
			t.Fatalf("PNG chunk at %d overruns the file", i)
		}
		typ := string(b[i+4 : i+8])
		data := b[i+8 : i+8+n]
		want := crc32.ChecksumIEEE(b[i+4 : i+8+n])
		if got := binary.BigEndian.Uint32(b[i+8+n:]); got != want {
			t.Fatalf("PNG chunk %s has a bad CRC", typ)
		}
		out = append(out, pngChunk{typ, data})
		i += 12 + n
	}
	return out
}

func buildPNG(chunks []pngChunk) []byte {
	out := []byte("\x89PNG\r\n\x1a\n")
	for _, c := range chunks {
		var h [4]byte
		binary.BigEndian.PutUint32(h[:], uint32(len(c.data)))
		out = append(out, h[:]...)
		body := append([]byte(c.typ), c.data...)
		out = append(out, body...)
		binary.BigEndian.PutUint32(h[:], crc32.ChecksumIEEE(body))
		out = append(out, h[:]...)
	}
	return out
}

// pngWithMetadata inserts the five chunk kinds files.md says are dropped, plus pHYs, which the
// spec does not list and so must survive.
func pngWithMetadata(t testing.TB) []byte {
	t.Helper()
	var out []pngChunk
	inserted := false
	for _, c := range parsePNG(t, plainPNG(t)) {
		if c.typ == "IDAT" && !inserted {
			inserted = true
			out = append(out,
				pngChunk{"pHYs", []byte{0, 0, 0x0b, 0x13, 0, 0, 0x0b, 0x13, 1}},
				pngChunk{"eXIf", append([]byte("MM\x00*"), []byte(markGPS+markMake)...)},
				pngChunk{"tEXt", []byte("Comment\x00" + markText)},
				pngChunk{"zTXt", append([]byte("Comment\x00\x00"), []byte(markText)...)},
				pngChunk{"iTXt", []byte("XML:com.adobe.xmp\x00\x00\x00\x00\x00" + markXMP)},
				pngChunk{"tIME", []byte{0x07, 0xea, 10, 20, 12, 34, 56}},
			)
		}
		out = append(out, c)
	}
	return buildPNG(out)
}

func idatBytes(chunks []pngChunk) []byte {
	var b []byte
	for _, c := range chunks {
		if c.typ == "IDAT" {
			b = append(b, c.data...)
		}
	}
	return b
}

func chunkTypes(chunks []pngChunk) []string {
	var out []string
	for _, c := range chunks {
		out = append(out, c.typ)
	}
	return out
}

// ---- JPEG ----

type jpegSegment struct {
	marker  byte
	payload []byte
}

func plainJPEG(t testing.TB) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, pattern(16, 16), &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return b.Bytes()
}

// jpegHeader parses the marker segments up to the start of scan and returns them with the rest
// of the file (from the SOS marker to the end: the entropy-coded pixels).
func jpegHeader(t testing.TB, b []byte) (segs []jpegSegment, scan []byte) {
	t.Helper()
	if len(b) < 4 || b[0] != 0xff || b[1] != 0xd8 {
		t.Fatalf("not a JPEG (starts %x)", b[:min(4, len(b))])
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xff {
			t.Fatalf("JPEG: expected a marker at %d, found %#x", i, b[i])
		}
		m := b[i+1]
		if m == 0xda {
			return segs, b[i:]
		}
		n := int(binary.BigEndian.Uint16(b[i+2:]))
		if n < 2 || i+2+n > len(b) {
			t.Fatalf("JPEG: segment %#x at %d overruns the file", m, i)
		}
		segs = append(segs, jpegSegment{m, b[i+4 : i+2+n]})
		i += 2 + n
	}
	t.Fatal("JPEG: no start of scan")
	return nil, nil
}

func segBytes(marker byte, payload []byte) []byte {
	out := []byte{0xff, marker, 0, 0}
	binary.BigEndian.PutUint16(out[2:], uint16(len(payload)+2))
	return append(out, payload...)
}

// buildExif makes a little-endian Exif payload ("Exif\0\0" + TIFF) with Make, DateTime and a
// GPS IFD, plus Orientation when orientation is not 0.
func buildExif(orientation uint16) []byte {
	le := binary.LittleEndian
	ent := func(tag, typ uint16, count, val uint32) []byte {
		b := make([]byte, 12)
		le.PutUint16(b, tag)
		le.PutUint16(b[2:], typ)
		le.PutUint32(b[4:], count)
		le.PutUint32(b[8:], val)
		return b
	}
	even := func(s string) []byte {
		b := append([]byte(s), 0)
		if len(b)%2 == 1 {
			b = append(b, 0)
		}
		return b
	}
	n := 3 // Make, DateTime, GPSInfo
	if orientation != 0 {
		n++
	}
	ifd0Len := 2 + 12*n + 4
	dataStart := 8 + ifd0Len
	makeBlob, dateBlob := even(markMake), even(markDate)
	gpsOff := dataStart + len(makeBlob) + len(dateBlob)
	datumBlob := even(markGPS)

	var ifd0 []byte
	ifd0 = le.AppendUint16(ifd0, uint16(n))
	ifd0 = append(ifd0, ent(0x010f, 2, uint32(len(makeBlob)), uint32(dataStart))...)
	if orientation != 0 {
		ifd0 = append(ifd0, ent(0x0112, 3, 1, uint32(orientation))...)
	}
	ifd0 = append(ifd0, ent(0x0132, 2, uint32(len(dateBlob)), uint32(dataStart+len(makeBlob)))...)
	ifd0 = append(ifd0, ent(0x8825, 4, 1, uint32(gpsOff))...)
	ifd0 = le.AppendUint32(ifd0, 0)

	var gps []byte
	gps = le.AppendUint16(gps, 2)
	gps = append(gps, ent(0x0001, 2, 2, uint32('N'))...)
	gps = append(gps, ent(0x0012, 2, uint32(len(datumBlob)), uint32(gpsOff+2+24+4))...)
	gps = le.AppendUint32(gps, 0)

	tiff := []byte{'I', 'I', 0x2a, 0, 8, 0, 0, 0}
	tiff = append(tiff, ifd0...)
	tiff = append(tiff, makeBlob...)
	tiff = append(tiff, dateBlob...)
	tiff = append(tiff, gps...)
	tiff = append(tiff, datumBlob...)
	return append([]byte("Exif\x00\x00"), tiff...)
}

// exifIFD0 returns the IFD0 tags of an Exif APP1 payload (little- or big-endian), tag -> raw
// 4-byte value field, or false when payload is not Exif.
func exifIFD0(payload []byte) (map[uint16]uint32, bool) {
	if !bytes.HasPrefix(payload, []byte("Exif\x00\x00")) || len(payload) < 6+8 {
		return nil, false
	}
	tiff := payload[6:]
	var bo binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, false
	}
	off := int(bo.Uint32(tiff[4:]))
	if off+2 > len(tiff) {
		return nil, false
	}
	n := int(bo.Uint16(tiff[off:]))
	tags := map[uint16]uint32{}
	for i := range n {
		e := off + 2 + 12*i
		if e+12 > len(tiff) {
			return nil, false
		}
		tag := bo.Uint16(tiff[e:])
		typ := bo.Uint16(tiff[e+2:])
		if typ == 3 { // SHORT inline: the value is in the first two bytes
			tags[tag] = uint32(bo.Uint16(tiff[e+8:]))
		} else {
			tags[tag] = bo.Uint32(tiff[e+8:])
		}
	}
	return tags, true
}

// jpegWithMetadata builds a JPEG carrying APP0 (JFIF), APP2 (ICC) and APP14 (Adobe), which
// must survive, and Exif (with orientation), XMP, COM and APP13, which must not (Exif is
// replaced by a minimal APP1 holding only a non-1 orientation).
func jpegWithMetadata(t testing.TB, orientation uint16) []byte {
	t.Helper()
	base := plainJPEG(t)
	var hdr []byte
	hdr = append(hdr, segBytes(0xe0, []byte("JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00"))...)
	hdr = append(hdr, segBytes(0xe2, append([]byte("ICC_PROFILE\x00\x01\x01"), bytes.Repeat([]byte{0x42}, 24)...))...)
	hdr = append(hdr, segBytes(0xee, []byte("Adobe\x00\x64\x00\x00\x00\x00\x01"))...)
	hdr = append(hdr, segBytes(0xe1, buildExif(orientation))...)
	hdr = append(hdr, segBytes(0xe1, []byte("http://ns.adobe.com/xap/1.0/\x00<x:xmpmeta>"+markXMP+"</x:xmpmeta>"))...)
	hdr = append(hdr, segBytes(0xfe, []byte(markText+" comment"))...)
	hdr = append(hdr, segBytes(0xed, []byte("Photoshop 3.0\x00"+markPhotos))...)
	out := append([]byte{0xff, 0xd8}, hdr...)
	return append(out, base[2:]...)
}

// ---- GIF ----

func plainGIF(t testing.TB) []byte {
	t.Helper()
	pal := color.Palette{color.Black, color.White, color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}}
	img := image.NewPaletted(image.Rect(0, 0, 8, 8), pal)
	for y := range 8 {
		for x := range 8 {
			img.SetColorIndex(x, y, uint8((x+2*y)%4))
		}
	}
	var b bytes.Buffer
	if err := gif.Encode(&b, img, nil); err != nil {
		t.Fatalf("gif.Encode: %v", err)
	}
	return b.Bytes()
}

// gifWithComment inserts a comment extension after the global colour table.
func gifWithComment(t testing.TB) []byte {
	t.Helper()
	b := plainGIF(t)
	pos := 13
	if b[10]&0x80 != 0 {
		pos += 3 * (1 << ((b[10] & 7) + 1))
	}
	ext := append([]byte{0x21, 0xfe, byte(len(markText))}, []byte(markText)...)
	ext = append(ext, 0)
	out := append([]byte{}, b[:pos]...)
	out = append(out, ext...)
	return append(out, b[pos:]...)
}

// ---- WebP ----

type riffChunk struct {
	id   string
	data []byte
}

func buildWebP(chunks ...riffChunk) []byte {
	var body []byte
	body = append(body, "WEBP"...)
	for _, c := range chunks {
		body = append(body, c.id...)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(c.data)))
		body = append(body, c.data...)
		if len(c.data)%2 == 1 {
			body = append(body, 0)
		}
	}
	out := []byte("RIFF")
	out = binary.LittleEndian.AppendUint32(out, uint32(len(body)))
	return append(out, body...)
}

func parseWebP(t testing.TB, b []byte) []riffChunk {
	t.Helper()
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		t.Fatalf("not a WebP (starts %q)", b[:min(12, len(b))])
	}
	if got := int(binary.LittleEndian.Uint32(b[4:])); got != len(b)-8 {
		t.Fatalf("RIFF size field = %d, want %d", got, len(b)-8)
	}
	var out []riffChunk
	for i := 12; i < len(b); {
		if i+8 > len(b) {
			t.Fatalf("truncated RIFF chunk header at %d", i)
		}
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		if i+8+n > len(b) {
			t.Fatalf("RIFF chunk %q overruns the file", b[i:i+4])
		}
		out = append(out, riffChunk{string(b[i : i+4]), b[i+8 : i+8+n]})
		i += 8 + n + n%2
	}
	return out
}

// webpVP8X is the extended header: EXIF and XMP flags set, a 8x8 canvas.
var webpVP8X = []byte{0x0c, 0, 0, 0, 7, 0, 0, 7, 0, 0}

// webpImage data is opaque to a stripper that copies chunks: 0x2f is the VP8L signature byte.
var webpVP8L = []byte{0x2f, 0x07, 0xc0, 0x01, 0x00, 0x10, 0x20, 0x30, 0x40, 0x50}

func plainWebP() []byte {
	return buildWebP(riffChunk{"VP8X", append([]byte{0}, webpVP8X[1:]...)}, riffChunk{"VP8L", webpVP8L})
}

func webpWithMetadata() []byte {
	return buildWebP(
		riffChunk{"VP8X", webpVP8X},
		riffChunk{"VP8L", webpVP8L},
		riffChunk{"EXIF", []byte("Exif\x00\x00" + markGPS + markMake)},
		riffChunk{"XMP ", []byte("<x:xmpmeta>" + markXMP + "</x:xmpmeta>")},
	)
}

// ---- PDF and refused types ----

func pdfDoc(extra string) []byte {
	return []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Author (Invented Author) >>\nendobj\n" + extra + "trailer\n<< /Root 1 0 R >>\n%%EOF\n")
}

// pdfOfSize is a PDF of exactly n bytes, padded with a comment line.
func pdfOfSize(t testing.TB, n int) []byte {
	t.Helper()
	head, tail := "%PDF-1.4\n", "trailer\n<< >>\n%%EOF\n"
	mid := n - len(head) - len(tail)
	if mid < 2 {
		t.Fatalf("pdfOfSize(%d): too small", n)
	}
	out := []byte(head + "%" + strings.Repeat("x", mid-2) + "\n" + tail)
	if len(out) != n {
		t.Fatalf("pdfOfSize: built %d bytes, want %d", len(out), n)
	}
	return out
}

var refusedBodies = map[string][]byte{
	"html named png":     []byte("<!DOCTYPE html><html><body><script>alert(1)</script></body></html>"),
	"html fragment":      []byte("<script>alert(1)</script>"),
	"svg":                []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><script>alert(1)</script></svg>`),
	"svg with xml":       []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`),
	"xml":                []byte(`<?xml version="1.0"?><root/>`),
	"plain text":         []byte("just some invented text, no magic bytes at all"),
	"zip":                []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00 not really a zip"),
	"docx-like zip":      []byte("PK\x03\x04\x14\x00\x06\x00\x08\x00[Content_Types].xml"),
	"heic":               []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00mif1heic"),
	"pdf with junk lead": append([]byte("junk\n"), pdfDoc("")...),
	"pdf missing dash":   []byte("%PDF1.4 not quite"),
	"riff but wave":      []byte("RIFF\x24\x00\x00\x00WAVEfmt "),
	"elf":                []byte("\x7fELF\x02\x01\x01\x00"),
	"png truncated sig":  []byte("\x89PNG\r\n"),
}
