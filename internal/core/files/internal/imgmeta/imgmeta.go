// Package imgmeta rewrites an image without its metadata, by copying the segments, chunks
// and blocks that make the picture and dropping the ones that describe where, when and with
// what it was taken. It decodes no pixels: image data is copied byte for byte, so the
// picture is identical and the cost is one pass.
//
// What is dropped (files.md, "Types"): PNG eXIf, tEXt, zTXt, iTXt and tIME chunks; every JPEG
// APPn and COM segment except APP0 (JFIF), APP2 (ICC) and APP14 (Adobe), keeping a minimal
// APP1 with only the Exif Orientation when it is not 1; GIF comment and non-animation
// application extensions; WebP EXIF and XMP chunks (and their flags in VP8X). Anything after
// the end of the picture (after IEND, EOI, the GIF trailer or the RIFF size) is dropped too.
//
// A structure the stripper cannot follow is ErrMalformed; the caller discards the output.
package imgmeta

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
)

// ErrMalformed is returned for an image whose structure cannot be followed to its end:
// truncated, wrong signature, a length that runs past the data, a bad checksum.
var ErrMalformed = errors.New("imgmeta: malformed image")

// Format names an image container.
type Format uint8

// The formats.
const (
	PNG Format = iota + 1
	JPEG
	GIF
	WebP
)

// Strip copies src to dst without the metadata of format f. dst must be seekable because a
// WebP header is patched with the final size. It returns ErrMalformed (wrapped) when src is
// not a whole, well-formed image of that format, and the reader's or writer's own error for
// I/O failures.
func Strip(f Format, dst io.WriteSeeker, src io.Reader) error {
	br := bufio.NewReaderSize(src, 64<<10)
	bw := bufio.NewWriterSize(dst, 64<<10)
	var err error
	switch f {
	case PNG:
		err = stripPNG(bw, br)
	case JPEG:
		err = stripJPEG(bw, br)
	case GIF:
		err = stripGIF(bw, br)
	case WebP:
		err = stripWebP(bw, br, dst)
	default:
		return fmt.Errorf("imgmeta: unknown format %d", f)
	}
	if err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("imgmeta: write: %w", err)
	}
	return nil
}

// malformed wraps ErrMalformed with what was wrong (never content).
func malformed(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrMalformed, fmt.Sprintf(format, args...))
}

// short turns an end of data in the middle of a structure into ErrMalformed and leaves
// every other error alone.
func short(err error, what string) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return malformed("truncated in %s", what)
	}
	return err
}

// ---- PNG ----

var pngSig = []byte("\x89PNG\r\n\x1a\n")

func dropPNG(typ string) bool {
	switch typ {
	case "eXIf", "tEXt", "zTXt", "iTXt", "tIME":
		return true
	}
	return false
}

func stripPNG(w *bufio.Writer, r *bufio.Reader) error {
	sig := make([]byte, len(pngSig))
	if _, err := io.ReadFull(r, sig); err != nil || !bytes.Equal(sig, pngSig) {
		return malformed("not a PNG")
	}
	if _, err := w.Write(sig); err != nil {
		return fmt.Errorf("imgmeta: write: %w", err)
	}
	first := true
	for {
		var head [8]byte
		if _, err := io.ReadFull(r, head[:]); err != nil {
			return short(err, "a PNG chunk header")
		}
		n := binary.BigEndian.Uint32(head[:4])
		typ := string(head[4:])
		if n > 1<<31-1 {
			return malformed("PNG chunk too long")
		}
		if first && typ != "IHDR" {
			return malformed("PNG does not start with IHDR")
		}
		first = false
		for _, c := range []byte(typ) {
			if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
				return malformed("PNG chunk type")
			}
		}
		crc := crc32.NewIEEE()
		crc.Write(head[4:]) //nolint:errcheck,gosec // hash.Hash.Write never returns an error
		keep := !dropPNG(typ)
		if keep {
			if _, err := w.Write(head[:]); err != nil {
				return fmt.Errorf("imgmeta: write: %w", err)
			}
		}
		dst := io.Writer(io.Discard)
		if keep {
			dst = w
		}
		if _, err := io.CopyN(io.MultiWriter(dst, crc), r, int64(n)); err != nil {
			if keepWriteError(err) {
				return fmt.Errorf("imgmeta: copy: %w", err)
			}
			return short(err, "a PNG chunk")
		}
		var tail [4]byte
		if _, err := io.ReadFull(r, tail[:]); err != nil {
			return short(err, "a PNG chunk checksum")
		}
		if binary.BigEndian.Uint32(tail[:]) != crc.Sum32() {
			return malformed("PNG chunk checksum")
		}
		if keep {
			if _, err := w.Write(tail[:]); err != nil {
				return fmt.Errorf("imgmeta: write: %w", err)
			}
		}
		if typ == "IEND" {
			return nil
		}
	}
}

// keepWriteError reports whether err from a CopyN came from the writer or a real read
// failure rather than the data ending early. CopyN reports a short source as io.EOF.
func keepWriteError(err error) bool {
	return !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF)
}

// ---- JPEG ----

func stripJPEG(w *bufio.Writer, r *bufio.Reader) error {
	var soi [2]byte
	if _, err := io.ReadFull(r, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return malformed("not a JPEG")
	}
	if _, err := w.Write(soi[:]); err != nil {
		return fmt.Errorf("imgmeta: write: %w", err)
	}
	wroteExif := false
	sawScan := false
	var pending byte // a marker copyScan already read
	for {
		m := pending
		pending = 0
		if m == 0 {
			var err error
			if m, err = nextMarker(r); err != nil {
				return short(err, "a JPEG marker")
			}
		}
		switch {
		case m == 0xD9: // EOI
			if !sawScan {
				return malformed("JPEG has no scan")
			}
			_, werr := w.Write([]byte{0xFF, 0xD9})
			return wrapWrite(werr)
		case m == 0xD8 || m == 0x01 || (m >= 0xD0 && m <= 0xD7):
			return malformed("unexpected JPEG marker")
		}
		var lb [2]byte
		if _, err := io.ReadFull(r, lb[:]); err != nil {
			return short(err, "a JPEG segment length")
		}
		n := int(binary.BigEndian.Uint16(lb[:]))
		if n < 2 {
			return malformed("JPEG segment length")
		}
		body := make([]byte, n-2)
		if _, err := io.ReadFull(r, body); err != nil {
			return short(err, "a JPEG segment")
		}
		keep := true
		var replace []byte
		switch {
		case m == 0xE1: // APP1: Exif or XMP
			keep = false
			if o, ok := exifOrientation(body); ok && o != 1 && !wroteExif {
				replace = orientationSegment(o)
				wroteExif = true
			}
		case m >= 0xE0 && m <= 0xEF: // other APPn
			keep = m == 0xE0 || m == 0xE2 || m == 0xEE
		case m == 0xFE: // COM
			keep = false
		}
		switch {
		case keep:
			for _, part := range [][]byte{{0xFF, m}, lb[:], body} {
				if _, err := w.Write(part); err != nil {
					return wrapWrite(err)
				}
			}
		case replace != nil:
			if _, err := w.Write(replace); err != nil {
				return wrapWrite(err)
			}
		}
		if m == 0xDA { // SOS: entropy-coded data follows, up to the next real marker
			sawScan = true
			var err error
			if pending, err = copyScan(w, r); err != nil {
				return err
			}
		}
	}
}

func wrapWrite(err error) error {
	if err != nil {
		return fmt.Errorf("imgmeta: write: %w", err)
	}
	return nil
}

// nextMarker reads the next marker code: an 0xFF, any fill 0xFF bytes, then a code that is
// neither 0x00 nor 0xFF.
func nextMarker(r *bufio.Reader) (byte, error) {
	b, err := r.ReadByte()
	if err != nil {
		return 0, err //nolint:wrapcheck // short() classifies it
	}
	if b != 0xFF {
		return 0, malformed("JPEG marker expected")
	}
	for {
		b, err = r.ReadByte()
		if err != nil {
			return 0, err //nolint:wrapcheck // short() classifies it
		}
		if b == 0xFF {
			continue
		}
		if b == 0x00 {
			return 0, malformed("JPEG marker expected")
		}
		return b, nil
	}
}

// copyScan copies entropy-coded data, which holds 0xFF only as 0xFF00 (a stuffed byte) or
// 0xFFD0..0xFFD7 (a restart), up to the next real marker, which it consumes and returns.
func copyScan(w *bufio.Writer, r *bufio.Reader) (byte, error) {
	for {
		chunk, err := r.ReadSlice(0xFF)
		switch {
		case err == nil:
			chunk = chunk[:len(chunk)-1] // the 0xFF is decided below
		case errors.Is(err, bufio.ErrBufferFull):
		default:
			return 0, short(err, "JPEG scan data")
		}
		if len(chunk) > 0 {
			if _, werr := w.Write(chunk); werr != nil {
				return 0, wrapWrite(werr)
			}
		}
		if err != nil {
			continue // the buffer filled with data and no 0xFF: keep copying
		}
		c, rerr := r.ReadByte()
		if rerr != nil {
			return 0, short(rerr, "JPEG scan data")
		}
		for c == 0xFF { // fill bytes before a marker
			if c, rerr = r.ReadByte(); rerr != nil {
				return 0, short(rerr, "JPEG scan data")
			}
		}
		if c == 0x00 || (c >= 0xD0 && c <= 0xD7) {
			if _, werr := w.Write([]byte{0xFF, c}); werr != nil {
				return 0, wrapWrite(werr)
			}
			continue
		}
		return c, nil
	}
}

// exifOrientation reads the Orientation tag (0x0112) of IFD0 from an APP1 body, if the body
// is Exif ("Exif\0\0" then a TIFF header). ok is false for XMP, a damaged Exif or no tag.
func exifOrientation(body []byte) (uint16, bool) {
	const hdr = "Exif\x00\x00"
	if !bytes.HasPrefix(body, []byte(hdr)) {
		return 0, false
	}
	t := body[len(hdr):]
	if len(t) < 8 {
		return 0, false
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 0, false
	}
	if bo.Uint16(t[2:4]) != 42 {
		return 0, false
	}
	ifd := int(bo.Uint32(t[4:8]))
	if ifd < 8 || ifd+2 > len(t) {
		return 0, false
	}
	count := int(bo.Uint16(t[ifd : ifd+2]))
	for i := range count {
		e := ifd + 2 + i*12
		if e+12 > len(t) {
			return 0, false
		}
		if bo.Uint16(t[e:e+2]) == 0x0112 && bo.Uint16(t[e+2:e+4]) == 3 && bo.Uint32(t[e+4:e+8]) == 1 {
			v := bo.Uint16(t[e+8 : e+10])
			if v >= 1 && v <= 8 {
				return v, true
			}
			return 0, false
		}
	}
	return 0, false
}

// orientationSegment is an APP1 segment holding an Exif block with only the Orientation tag.
func orientationSegment(o uint16) []byte {
	var b bytes.Buffer
	b.Write([]byte{0xFF, 0xE1, 0x00, 0x22}) // marker, length 34
	b.WriteString("Exif\x00\x00")
	b.Write([]byte{'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00}) // little-endian TIFF header, IFD0 at 8
	b.Write([]byte{0x01, 0x00})                                   // one entry
	b.Write([]byte{0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00})
	b.Write(binary.LittleEndian.AppendUint16(nil, o))
	b.Write([]byte{0x00, 0x00})
	b.Write([]byte{0x00, 0x00, 0x00, 0x00}) // no next IFD
	return b.Bytes()
}

// ---- GIF ----

func stripGIF(w *bufio.Writer, r *bufio.Reader) error {
	var head [13]byte // signature, version, logical screen descriptor
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return malformed("not a GIF")
	}
	if s := string(head[:6]); s != "GIF87a" && s != "GIF89a" {
		return malformed("not a GIF")
	}
	if _, err := w.Write(head[:]); err != nil {
		return wrapWrite(err)
	}
	if head[10]&0x80 != 0 {
		if err := copyN(w, r, 3<<((head[10]&7)+1), "a GIF color table"); err != nil {
			return err
		}
	}
	sawImage := false
	for {
		b, err := r.ReadByte()
		if err != nil {
			return short(err, "a GIF block")
		}
		switch b {
		case 0x3B: // trailer
			if !sawImage {
				return malformed("GIF has no image")
			}
			return wrapWrite(w.WriteByte(0x3B))
		case 0x2C: // image descriptor
			sawImage = true
			var d [9]byte
			if _, err := io.ReadFull(r, d[:]); err != nil {
				return short(err, "a GIF image descriptor")
			}
			if err := w.WriteByte(0x2C); err != nil {
				return wrapWrite(err)
			}
			if _, err := w.Write(d[:]); err != nil {
				return wrapWrite(err)
			}
			if d[8]&0x80 != 0 {
				if err := copyN(w, r, 3<<((d[8]&7)+1), "a GIF color table"); err != nil {
					return err
				}
			}
			lzw, err := r.ReadByte()
			if err != nil {
				return short(err, "a GIF image")
			}
			if err := w.WriteByte(lzw); err != nil {
				return wrapWrite(err)
			}
			if err := copySubBlocks(w, r, true); err != nil {
				return err
			}
		case 0x21: // extension
			label, err := r.ReadByte()
			if err != nil {
				return short(err, "a GIF extension")
			}
			keep := true
			switch label {
			case 0xFE: // comment
				keep = false
			case 0xFF: // application: keep only the animation loop extensions
				keep, err = keepAppExtension(r)
				if err != nil {
					return err
				}
			}
			if keep {
				if err := w.WriteByte(0x21); err != nil {
					return wrapWrite(err)
				}
				if err := w.WriteByte(label); err != nil {
					return wrapWrite(err)
				}
				if label == 0xFF {
					if err := writeAppHeader(w, r); err != nil {
						return err
					}
				}
			}
			if err := copySubBlocks(w, r, keep); err != nil {
				return err
			}
		default:
			return malformed("GIF block")
		}
	}
}

// keepAppExtension peeks at the application identifier (the first sub-block) of an
// application extension and says whether it is one of the animation loop extensions.
func keepAppExtension(r *bufio.Reader) (bool, error) {
	n, err := r.Peek(1)
	if err != nil {
		return false, short(err, "a GIF application extension")
	}
	size := int(n[0])
	if size != 11 {
		return false, nil
	}
	id, err := r.Peek(1 + size)
	if err != nil {
		return false, short(err, "a GIF application extension")
	}
	switch string(id[1:]) {
	case "NETSCAPE2.0", "ANIMEXTS1.0":
		return true, nil
	}
	return false, nil
}

// writeAppHeader copies the fixed first sub-block (size byte and identifier) of an
// application extension that keepAppExtension accepted.
func writeAppHeader(w *bufio.Writer, r *bufio.Reader) error {
	return copyN(w, r, 12, "a GIF application extension")
}

// copySubBlocks copies (or with keep false, skips) sub-blocks up to and including the zero
// terminator. data is only informational.
func copySubBlocks(w *bufio.Writer, r *bufio.Reader, keep bool) error {
	for {
		n, err := r.ReadByte()
		if err != nil {
			return short(err, "GIF sub-blocks")
		}
		if keep {
			if err := w.WriteByte(n); err != nil {
				return wrapWrite(err)
			}
		}
		if n == 0 {
			return nil
		}
		dst := io.Writer(io.Discard)
		if keep {
			dst = w
		}
		if _, err := io.CopyN(dst, r, int64(n)); err != nil {
			if keepWriteError(err) {
				return fmt.Errorf("imgmeta: copy: %w", err)
			}
			return short(err, "a GIF sub-block")
		}
	}
}

func copyN(w io.Writer, r io.Reader, n int, what string) error {
	if _, err := io.CopyN(w, r, int64(n)); err != nil {
		if keepWriteError(err) {
			return fmt.Errorf("imgmeta: copy: %w", err)
		}
		return short(err, what)
	}
	return nil
}

// ---- WebP ----

func stripWebP(w *bufio.Writer, r *bufio.Reader, dst io.WriteSeeker) error {
	// The RIFF size sits before the chunks, so the header is written last: remember where
	// it goes, write a placeholder, and patch it once the size is known.
	var head [12]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return malformed("not a WebP")
	}
	if string(head[:4]) != "RIFF" || string(head[8:]) != "WEBP" {
		return malformed("not a WebP")
	}
	riff := int64(binary.LittleEndian.Uint32(head[4:8]))
	if riff < 4+8 {
		return malformed("WebP size")
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("imgmeta: write: %w", err)
	}
	start, err := dst.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("imgmeta: seek: %w", err)
	}
	if _, err := w.Write(head[:]); err != nil {
		return wrapWrite(err)
	}
	remaining := riff - 4 // the "WEBP" form type is counted in riff
	written := int64(4)
	sawImage := false
	for remaining > 0 {
		var ch [8]byte
		if remaining < 8 {
			return malformed("WebP chunk header")
		}
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return short(err, "a WebP chunk header")
		}
		size := int64(binary.LittleEndian.Uint32(ch[4:]))
		padded := size + size%2
		if 8+padded > remaining {
			return malformed("WebP chunk runs past the file")
		}
		remaining -= 8 + padded
		fourcc := string(ch[:4])
		switch fourcc {
		case "VP8 ", "VP8L", "ANMF":
			sawImage = true
		}
		if fourcc == "EXIF" || fourcc == "XMP " {
			if _, err := io.CopyN(io.Discard, r, padded); err != nil {
				return short(err, "a WebP chunk")
			}
			continue
		}
		if fourcc == "VP8X" {
			if size < 10 {
				return malformed("WebP VP8X chunk")
			}
			body := make([]byte, padded)
			if _, err := io.ReadFull(r, body); err != nil {
				return short(err, "a WebP chunk")
			}
			body[0] &^= 0x08 | 0x04 // clear the Exif and XMP flags
			if _, err := w.Write(ch[:]); err != nil {
				return wrapWrite(err)
			}
			if _, err := w.Write(body); err != nil {
				return wrapWrite(err)
			}
			written += 8 + padded
			continue
		}
		if _, err := w.Write(ch[:]); err != nil {
			return wrapWrite(err)
		}
		if _, err := io.CopyN(w, r, padded); err != nil {
			if keepWriteError(err) {
				return fmt.Errorf("imgmeta: copy: %w", err)
			}
			return short(err, "a WebP chunk")
		}
		written += 8 + padded
	}
	if !sawImage {
		return malformed("WebP has no image")
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("imgmeta: write: %w", err)
	}
	var size [4]byte
	binary.LittleEndian.PutUint32(size[:], uint32(written)) //nolint:gosec // bounded by the input's own 32-bit RIFF size
	end, err := dst.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("imgmeta: seek: %w", err)
	}
	if _, err := dst.Seek(start+4, io.SeekStart); err != nil {
		return fmt.Errorf("imgmeta: seek: %w", err)
	}
	if _, err := dst.Write(size[:]); err != nil {
		return fmt.Errorf("imgmeta: write: %w", err)
	}
	if _, err := dst.Seek(end, io.SeekStart); err != nil {
		return fmt.Errorf("imgmeta: seek: %w", err)
	}
	return nil
}
