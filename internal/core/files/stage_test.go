//nolint:wrapcheck // test code: errors come straight from the fake or store under test; fixtures do bounded arithmetic
package files_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/files"
	"github.com/zedgama3/bms/internal/core/objects"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// These tests cover Stage (files.md failure modes F3-F12) over dbtest.NewFake, an in-memory
// store and a real temp directory. Fixtures are generated in images_test.go.

func TestStage_AcceptedKinds_StoresSniffedTypeAndCleanName(t *testing.T) {
	jpg := plainJPEG(t)
	tests := []struct {
		name     string
		clientNm string
		body     []byte
		accept   files.Accept
		wantType string
		wantName string
		stripped bool
	}{
		{"png", "photo.png", plainPNG(t), files.Images, "image/png", "photo.png", true},
		{"jpeg", "photo.jpeg", jpg, files.Images, "image/jpeg", "photo.jpg", true},
		{"gif", "anim.gif", plainGIF(t), files.Images, "image/gif", "anim.gif", true},
		{"webp", "pic.webp", plainWebP(), files.Images, "image/webp", "pic.webp", true},
		{"pdf", "cert.pdf", pdfDoc(""), files.Documents, "application/pdf", "cert.pdf", false},
		{"pdf in images and documents", "cert.pdf", pdfDoc(""), append(append(files.Accept{}, files.Images...), files.PDF), "application/pdf", "cert.pdf", false},
		{"brand takes webp", "logo.webp", plainWebP(), files.Brand, "image/webp", "logo.webp", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			before := r.clk.Now()
			st, err := stageBytes(ctxT(t), r.vol, tc.clientNm, tc.accept, tc.body)
			wantNoErr(t, err)
			if st.ContentType != tc.wantType {
				t.Errorf("ContentType = %q, want %q", st.ContentType, tc.wantType)
			}
			if st.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", st.Name, tc.wantName)
			}
			if st.Bucket != ownerVol {
				t.Errorf("Bucket = %q, want the owner %q", st.Bucket, ownerVol)
			}
			if st.Stripped != tc.stripped {
				t.Errorf("Stripped = %v, want %v", st.Stripped, tc.stripped)
			}
			if !st.StagedAt.Equal(before) {
				t.Errorf("StagedAt = %v, want the clock's now %v", st.StagedAt, before)
			}
			if err := st.Validate(); err != nil {
				t.Errorf("Staged.Validate: %v", err)
			}
			if _, err := model.ParseID[files.FileID](string(st.Key)); err != nil {
				t.Errorf("Key %q is not a file ID: %v", st.Key, err)
			}
			stored := r.objectBytes(st.Bucket, st.Key)
			if int64(len(stored)) != st.Size {
				t.Errorf("stored %d bytes, Staged.Size = %d", len(stored), st.Size)
			}
			if sha256.Sum256(stored) != st.SHA256 {
				t.Error("Staged.SHA256 is not the checksum of the stored bytes")
			}
			info, err := r.inner.Head(ctxT(t), st.Bucket, st.Key)
			wantNoErr(t, err)
			if info.ContentType != tc.wantType {
				t.Errorf("stored object ContentType = %q, want the sniffed %q", info.ContentType, tc.wantType)
			}
			if !tc.stripped && !bytes.Equal(stored, tc.body) {
				t.Error("a PDF was rewritten; it must be stored byte for byte")
			}
			r.wantNoTempFiles()
			if keys := r.objectKeys(ownerInc); len(keys) != 0 {
				t.Errorf("other owner's bucket holds %v", keys)
			}
		})
	}
}

func TestStage_TwoCalls_AreTwoObjects(t *testing.T) {
	r := newRig(t)
	a := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	b := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	if a.Key == b.Key {
		t.Fatalf("two Stage calls produced the same key %q", a.Key)
	}
	if got := len(r.objectKeys(ownerVol)); got != 2 {
		t.Errorf("objects = %d, want 2 (no deduplication)", got)
	}
}

// Decision 7: the stored type comes from the bytes, never from the name or the declared type.
func TestStage_SniffedTypeBeatsClientName(t *testing.T) {
	r := newRig(t)
	st := mustStage(t, r.vol, "totally-a-document.pdf", files.Images, plainPNG(t))
	if st.ContentType != "image/png" {
		t.Errorf("ContentType = %q, want image/png (sniffed)", st.ContentType)
	}
	if st.Name != "totally-a-document.png" {
		t.Errorf("Name = %q, want the extension forced to .png", st.Name)
	}
	st = mustStage(t, r.vol, "photo.PNG", files.Images, plainJPEG(t))
	if st.ContentType != "image/jpeg" || st.Name != "photo.jpg" {
		t.Errorf("got %q / %q, want image/jpeg / photo.jpg", st.ContentType, st.Name)
	}
}

func TestStage_NameCleaning(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"../../etc/passwd.png", "passwd.png"},
		{`C:\Users\someone\holiday.png`, "holiday.png"},
		{"  spaced.png  ", "spaced.png"},
		{"", "file.png"},
		{"noextension", "noextension.png"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			r := newRig(t)
			st, err := stageBytes(ctxT(t), r.vol, tc.in, files.Images, plainPNG(t))
			wantNoErr(t, err)
			want := tc.want
			if want == "" {
				want = "file.png"
			}
			if st.Name != want {
				t.Errorf("Name(%q) = %q, want %q", tc.in, st.Name, want)
			}
			if strings.ContainsAny(st.Name, `/\`) {
				t.Errorf("Name %q contains a path separator", st.Name)
			}
		})
	}
}

// F4
func TestStage_BadUpload_ReturnsInvalid(t *testing.T) {
	long := strings.Repeat("n", files.MaxNameBytes+1) + ".png"
	tests := []struct {
		name         string
		up           files.Upload
		field, code  string
		wantNoReader bool
	}{
		{"empty accept", files.Upload{Name: "a.png", Accept: nil}, "accept", "required", false},
		{"zero kind in accept", files.Upload{Name: "a.png", Accept: files.Accept{0}}, "accept", "", false},
		{"unknown kind in accept", files.Upload{Name: "a.png", Accept: files.Accept{files.Kind(99)}}, "accept", "", false},
		{"name too long", files.Upload{Name: long, Accept: files.Images}, "name", "too_long", false},
		{"nul in name", files.Upload{Name: "a\x00b.png", Accept: files.Images}, "name", "control_chars", false},
		{"newline in name", files.Upload{Name: "a\nb.png", Accept: files.Images}, "name", "control_chars", false},
		{"escape in name", files.Upload{Name: "a\x1bb.png", Accept: files.Images}, "name", "control_chars", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			tc.up.Body = bytes.NewReader(plainPNG(t))
			_, err := r.vol.Stage(ctxT(t), tc.up)
			if tc.code != "" {
				fe := fieldErr(t, err, errs.Invalid, tc.field, tc.code)
				if tc.code == "too_long" && fe.Params["max"] != itoa(files.MaxNameBytes) {
					t.Errorf("too_long max param = %q, want %d", fe.Params["max"], files.MaxNameBytes)
				}
			} else {
				wantKind(t, err, errs.Invalid)
			}
			// Upload.Validate says the same without touching the body.
			if verr := (files.Upload{Name: tc.up.Name, Accept: tc.up.Accept}).Validate(); !errs.Is(verr, errs.Invalid) {
				t.Errorf("Upload.Validate = %v, want Invalid", verr)
			}
			r.wantNoObjects(ownerVol)
			r.wantNoTempFiles()
		})
	}
}

func TestUploadValidate_Good(t *testing.T) {
	if err := (files.Upload{Name: "a.png", Accept: files.Images}).Validate(); err != nil {
		t.Errorf("Validate(good) = %v", err)
	}
	if err := (files.Upload{Name: "", Accept: files.Documents}).Validate(); err != nil {
		t.Errorf("Validate(empty name) = %v; an empty name becomes \"file\"", err)
	}
	if err := (files.Upload{Name: strings.Repeat("n", files.MaxNameBytes), Accept: files.Images}).Validate(); err != nil {
		t.Errorf("Validate(name of MaxNameBytes) = %v", err)
	}
}

// F3
func TestStage_InTx_ReturnsInternal(t *testing.T) {
	for _, mode := range []db.Mode{db.Read, db.Write} {
		t.Run(mode.String(), func(t *testing.T) {
			r := newRig(t)
			err := r.db.Transact(ctxT(t), mode, func(ctx context.Context) error {
				_, err := stageBytes(ctx, r.vol, "a.png", files.Images, plainPNG(t))
				return err
			})
			wantKind(t, err, errs.Internal)
			r.wantNoObjects(ownerVol)
			r.wantNoTempFiles()
		})
	}
}

// F5
func TestStage_EmptyBody_ReturnsInvalidRequired(t *testing.T) {
	r := newRig(t)
	_, err := r.vol.Stage(ctxT(t), files.Upload{Name: "a.png", Accept: files.Images, Body: bytes.NewReader(nil)})
	wantField(t, err, errs.Invalid, "file", "required")
	r.wantNoObjects(ownerVol)
	r.wantNoTempFiles()
}

// F6: the sniffed kind is not in Accept, whatever the name says.
func TestStage_UnacceptedType_ReturnsInvalidType(t *testing.T) {
	for name, body := range refusedBodies {
		for _, clientName := range []string{"innocent.png", "innocent.pdf", "innocent.jpg", "innocent.svg", "innocent.html"} {
			t.Run(name+"/"+clientName, func(t *testing.T) {
				r := newRig(t)
				_, err := stageBytes(ctxT(t), r.vol, clientName, files.Images, body)
				wantField(t, err, errs.Invalid, "file", "type")
				r.wantNoObjects(ownerVol)
				r.wantNoTempFiles()
			})
		}
	}
}

func TestStage_KindOutsideAccept_ReturnsInvalidType(t *testing.T) {
	tests := []struct {
		name   string
		body   []byte
		accept files.Accept
	}{
		{"pdf where images are wanted", pdfDoc(""), files.Images},
		{"png where documents are wanted", plainPNG(t), files.Documents},
		{"gif for a logo", plainGIF(t), files.Brand},
		{"jpeg where only pdf", plainJPEG(t), files.Documents},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			_, err := stageBytes(ctxT(t), r.vol, "x", tc.accept, tc.body)
			wantField(t, err, errs.Invalid, "file", "type")
			r.wantNoObjects(ownerVol)
		})
	}
}

// Decision 8: no SVG and no HTML even if a caller lists every kind there is.
func TestStage_SVGAndHTML_AreNeverAccepted(t *testing.T) {
	all := files.Accept{files.PNG, files.JPEG, files.GIF, files.WebP, files.PDF}
	r := newRig(t)
	for _, name := range []string{"svg", "html named png", "html fragment", "svg with xml"} {
		_, err := stageBytes(ctxT(t), r.vol, "x.svg", all, refusedBodies[name])
		wantField(t, err, errs.Invalid, "file", "type")
	}
	r.wantNoObjects(ownerVol)
}

// The type is decided from the first SniffBytes, before the rest of the body is read.
func TestStage_UnacceptedType_StopsReading(t *testing.T) {
	r := newRig(t)
	body := &infiniteBody{head: []byte("<!DOCTYPE html><html>")}
	_, err := r.vol.Stage(ctxT(t), files.Upload{Name: "evil.png", Accept: files.Images, Body: body})
	wantField(t, err, errs.Invalid, "file", "type")
	if limit := int64(files.DefaultPolicy().MaxBytes); body.read > limit {
		t.Errorf("read %d bytes of a refused upload; it must stop near the sniff window", body.read)
	}
	r.wantNoTempFiles()
}

// F7
func TestStage_OverMaxBytes_ReturnsInvalidTooLarge(t *testing.T) {
	const limit = 1 << 20
	r := newRig(t, withPolicy(files.Policy{MaxBytes: limit, MaxPerRecord: 20}))
	_, err := stageBytes(ctxT(t), r.vol, "big.pdf", files.Documents, pdfOfSize(t, limit+1))
	fe := fieldErr(t, err, errs.Invalid, "file", "too_large")
	if fe.Params["max"] != itoa(limit) {
		t.Errorf("max param = %q, want %d", fe.Params["max"], limit)
	}
	r.wantNoObjects(ownerVol)
	r.wantNoTempFiles()
}

func TestStage_ExactlyMaxBytes_IsAccepted(t *testing.T) {
	const limit = 1 << 20
	r := newRig(t, withPolicy(files.Policy{MaxBytes: limit, MaxPerRecord: 20}))
	body := pdfOfSize(t, limit)
	st, err := stageBytes(ctxT(t), r.vol, "just-fits.pdf", files.Documents, body)
	wantNoErr(t, err)
	if st.Size != limit {
		t.Errorf("Size = %d, want %d", st.Size, limit)
	}
	if !bytes.Equal(r.objectBytes(st.Bucket, st.Key), body) {
		t.Error("stored bytes differ")
	}
}

func TestStage_OverMaxBytes_StopsReadingAtTheLimit(t *testing.T) {
	const limit = 1 << 20
	r := newRig(t, withPolicy(files.Policy{MaxBytes: limit, MaxPerRecord: 20}))
	body := &infiniteBody{head: pdfDoc("")}
	_, err := r.vol.Stage(ctxT(t), files.Upload{Name: "endless.pdf", Accept: files.Documents, Body: body})
	wantField(t, err, errs.Invalid, "file", "too_large")
	if body.read > limit+(256<<10) {
		t.Errorf("read %d bytes past a limit of %d; the read must stop at the limit", body.read, limit)
	}
	r.wantNoTempFiles()
	r.wantNoObjects(ownerVol)
}

func TestStage_PolicyIsReadPerCall(t *testing.T) {
	r := newRig(t, withPolicy(files.Policy{MaxBytes: 1 << 20, MaxPerRecord: 20}))
	big := pdfOfSize(t, 2<<20)
	_, err := stageBytes(ctxT(t), r.vol, "a.pdf", files.Documents, big)
	wantField(t, err, errs.Invalid, "file", "too_large")
	r.policy.set(files.Policy{MaxBytes: 4 << 20, MaxPerRecord: 20}) // the regional raised the setting
	if _, err := stageBytes(ctxT(t), r.vol, "a.pdf", files.Documents, big); err != nil {
		t.Errorf("after raising max_bytes: %v", err)
	}
}

func TestStage_PolicySourceFails_ReturnsItsKind(t *testing.T) {
	r := newRig(t)
	r.policy.mu.Lock()
	r.policy.err = errs.E("test.policy", errs.Unavailable, errors.New("settings down"))
	r.policy.mu.Unlock()
	_, err := stageBytes(ctxT(t), r.vol, "a.png", files.Images, plainPNG(t))
	wantKind(t, err, errs.Unavailable)
	r.wantNoObjects(ownerVol)
}

// F8
func TestStage_CorruptImage_ReturnsInvalidMalformed(t *testing.T) {
	png := plainPNG(t)
	jpg := plainJPEG(t)
	tests := []struct {
		name string
		body []byte
	}{
		{"png chunk overruns file", append([]byte("\x89PNG\r\n\x1a\n"), 0xff, 0xff, 0xff, 0xf0, 'I', 'H', 'D', 'R', 1, 2, 3)},
		{"png cut in the middle", png[:len(png)/2]},
		{"png signature only", []byte("\x89PNG\r\n\x1a\n")},
		{"jpeg segment overruns file", append([]byte{0xff, 0xd8, 0xff, 0xe1, 0xff, 0xf0}, bytes.Repeat([]byte{1}, 40)...)},
		{"jpeg cut inside the header", jpg[:12]},
		{"jpeg soi only plus garbage", append([]byte{0xff, 0xd8, 0xff}, bytes.Repeat([]byte{0x00}, 600)...)},
		{"gif header only", []byte("GIF89a")},
		{"gif truncated", append([]byte("GIF89a\x08\x00\x08\x00\xf0\x00\x00"), 0x21, 0xfe, 0x40, 'a')},
		{"webp size field lies", append([]byte("RIFF\xf0\xff\xff\x7fWEBPVP8L\x0a\x00\x00\x00"), 0x2f, 1, 2)},
		{"webp chunk overruns", append([]byte("RIFF\x18\x00\x00\x00WEBPVP8X\xff\x00\x00\x00"), make([]byte, 8)...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			_, err := stageBytes(ctxT(t), r.vol, "broken", files.Images, tc.body)
			wantField(t, err, errs.Invalid, "file", "malformed")
			r.wantNoObjects(ownerVol)
			r.wantNoTempFiles()
		})
	}
}

// ---- metadata stripping (decision 9) ----

func TestStage_PNG_StripsMetadataKeepsPixels(t *testing.T) {
	r := newRig(t)
	in := pngWithMetadata(t)
	st := mustStage(t, r.vol, "p.png", files.Images, in)
	out := r.objectBytes(st.Bucket, st.Key)

	if !st.Stripped {
		t.Error("Stripped = false for an image stripped by default")
	}
	got := chunkTypes(parsePNG(t, out))
	for _, bad := range []string{"eXIf", "tEXt", "zTXt", "iTXt", "tIME"} {
		for _, g := range got {
			if g == bad {
				t.Errorf("chunk %s survived: %v", bad, got)
			}
		}
	}
	for _, mark := range []string{markText, markXMP, markGPS, markMake} {
		if bytes.Contains(out, []byte(mark)) {
			t.Errorf("metadata marker %q survived in the stored bytes", mark)
		}
	}
	if !contains(got, "pHYs") {
		t.Errorf("pHYs (not on the stripped list) was dropped: %v", got)
	}
	if !bytes.Equal(idatBytes(parsePNG(t, out)), idatBytes(parsePNG(t, in))) {
		t.Error("IDAT (the pixels) changed")
	}
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("stripped PNG no longer decodes: %v", err)
	}
	if st.Size != int64(len(out)) || sha256.Sum256(out) != st.SHA256 {
		t.Error("Staged size or checksum describes the original, not the stripped bytes")
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestStage_JPEG_StripsGPSKeepsOrientationAndPixels(t *testing.T) {
	r := newRig(t)
	in := jpegWithMetadata(t, 6)
	st := mustStage(t, r.vol, "phone.jpg", files.Images, in)
	out := r.objectBytes(st.Bucket, st.Key)

	for _, mark := range []string{markGPS, markMake, markDate, markXMP, markText, markPhotos, "xmpmeta"} {
		if bytes.Contains(out, []byte(mark)) {
			t.Errorf("metadata marker %q survived in the stored bytes", mark)
		}
	}
	inSegs, inScan := jpegHeader(t, in)
	outSegs, outScan := jpegHeader(t, out)
	if !bytes.Equal(inScan, outScan) {
		t.Error("the entropy-coded scan data changed; the pixels must be byte-identical")
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("stripped JPEG no longer decodes: %v", err)
	}

	byMarker := map[byte][][]byte{}
	for _, s := range outSegs {
		byMarker[s.marker] = append(byMarker[s.marker], s.payload)
	}
	for _, keep := range []byte{0xe0, 0xe2, 0xee} { // APP0 JFIF, APP2 ICC, APP14 Adobe
		var want []byte
		for _, s := range inSegs {
			if s.marker == keep {
				want = s.payload
			}
		}
		if len(byMarker[keep]) != 1 || !bytes.Equal(byMarker[keep][0], want) {
			t.Errorf("segment %#x was not kept unchanged", keep)
		}
	}
	for _, drop := range []byte{0xfe, 0xed} { // COM, APP13
		if len(byMarker[drop]) != 0 {
			t.Errorf("segment %#x survived", drop)
		}
	}
	// APP1: at most one segment, an Exif holding only Orientation = 6.
	if len(byMarker[0xe1]) != 1 {
		t.Fatalf("APP1 segments = %d, want exactly one minimal Exif with the orientation", len(byMarker[0xe1]))
	}
	tags, ok := exifIFD0(byMarker[0xe1][0])
	if !ok {
		t.Fatal("the remaining APP1 is not a readable Exif")
	}
	if len(tags) != 1 {
		t.Errorf("Exif tags kept = %v, want only Orientation (0x0112)", tags)
	}
	if tags[0x0112] != 6 {
		t.Errorf("Orientation = %d, want 6 kept", tags[0x0112])
	}
	if _, gps := tags[0x8825]; gps {
		t.Error("GPSInfo pointer survived")
	}
}

func TestStage_JPEG_WithoutOrientation_LeavesNoExif(t *testing.T) {
	for _, orientation := range []uint16{0, 1} {
		t.Run(itoa(int64(orientation)), func(t *testing.T) {
			r := newRig(t)
			in := jpegWithMetadata(t, orientation)
			st := mustStage(t, r.vol, "phone.jpg", files.Images, in)
			out := r.objectBytes(st.Bucket, st.Key)
			for _, mark := range []string{markGPS, markMake, markDate} {
				if bytes.Contains(out, []byte(mark)) {
					t.Errorf("marker %q survived", mark)
				}
			}
			segs, _ := jpegHeader(t, out)
			for _, s := range segs {
				if s.marker != 0xe1 {
					continue
				}
				tags, ok := exifIFD0(s.payload)
				if !ok {
					t.Error("a non-Exif APP1 survived")
					continue
				}
				if len(tags) > 1 || (len(tags) == 1 && tags[0x0112] != 1) {
					t.Errorf("Exif with tags %v survived for orientation %d", tags, orientation)
				}
			}
		})
	}
}

func TestStage_GIF_DropsCommentExtension(t *testing.T) {
	r := newRig(t)
	in := gifWithComment(t)
	if !bytes.Contains(in, []byte(markText)) {
		t.Fatal("fixture has no comment")
	}
	st := mustStage(t, r.vol, "a.gif", files.Images, in)
	out := r.objectBytes(st.Bucket, st.Key)
	if bytes.Contains(out, []byte(markText)) {
		t.Error("GIF comment survived")
	}
	want, err := gif.DecodeAll(bytes.NewReader(in))
	wantNoErr(t, err)
	got, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("stripped GIF no longer decodes: %v", err)
	}
	if len(got.Image) != len(want.Image) || !bytes.Equal(got.Image[0].Pix, want.Image[0].Pix) {
		t.Error("GIF pixels changed")
	}
}

func TestStage_WebP_DropsEXIFAndXMPChunks(t *testing.T) {
	r := newRig(t)
	in := webpWithMetadata()
	st := mustStage(t, r.vol, "a.webp", files.Images, in)
	out := r.objectBytes(st.Bucket, st.Key)
	chunks := parseWebP(t, out) // also checks the RIFF size field
	var ids []string
	for _, c := range chunks {
		ids = append(ids, c.id)
	}
	if contains(ids, "EXIF") || contains(ids, "XMP ") {
		t.Errorf("chunks = %v, EXIF and XMP must be dropped", ids)
	}
	if !contains(ids, "VP8L") {
		t.Fatalf("chunks = %v, the image chunk is gone", ids)
	}
	for _, c := range chunks {
		if c.id == "VP8L" && !bytes.Equal(c.data, webpVP8L) {
			t.Error("image data changed")
		}
	}
	for _, mark := range []string{markGPS, markMake, markXMP} {
		if bytes.Contains(out, []byte(mark)) {
			t.Errorf("marker %q survived", mark)
		}
	}
}

func TestStage_KeepMetadata_LeavesBytesIdentical(t *testing.T) {
	inputs := map[string][]byte{
		"png":  pngWithMetadata(t),
		"jpeg": jpegWithMetadata(t, 6),
		"gif":  gifWithComment(t),
		"webp": webpWithMetadata(),
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			st, err := r.vol.Stage(ctxT(t), files.Upload{Name: "keep", Accept: files.Images, KeepMetadata: true, Body: bytes.NewReader(in)})
			wantNoErr(t, err)
			if st.Stripped {
				t.Error("Stripped = true although KeepMetadata was set")
			}
			if !bytes.Equal(r.objectBytes(st.Bucket, st.Key), in) {
				t.Error("KeepMetadata must leave the bytes identical")
			}
		})
	}
}

func TestStage_StrippedImage_StillDecodesAsImage(t *testing.T) {
	r := newRig(t)
	st := mustStage(t, r.vol, "p.png", files.Images, pngWithMetadata(t))
	img, _, err := image.Decode(bytes.NewReader(r.objectBytes(st.Bucket, st.Key)))
	wantNoErr(t, err)
	if b := img.Bounds(); b.Dx() != 8 || b.Dy() != 8 {
		t.Errorf("bounds = %v", b)
	}
}

// F10
func TestStage_TempDirUnwritable_ReturnsUnavailable(t *testing.T) {
	missing := t.TempDir() + "/does/not/exist"
	r := newRig(t, withTempDir(missing))
	_, err := stageBytes(ctxT(t), r.vol, "a.png", files.Images, plainPNG(t))
	wantKind(t, err, errs.Unavailable)
	r.wantNoObjects(ownerVol)
}

// F11
func TestStage_PutFails_ReturnsUnavailableNothingKept(t *testing.T) {
	cases := map[string]error{
		"store down":       unavailable("put"),
		"checksum refused": errs.E("test.Put", errs.Invalid, errors.New("checksum mismatch")),
		"disk full":        errs.E("test.Put", errs.Unavailable, errors.New("no space left")),
	}
	for name, putErr := range cases {
		t.Run(name, func(t *testing.T) {
			r := newRig(t)
			r.store.fail("put", putErr)
			_, err := stageBytes(ctxT(t), r.vol, "a.png", files.Images, plainPNG(t))
			wantKind(t, err, errs.Unavailable)
			r.wantNoObjects(ownerVol)
			r.wantNoTempFiles()
			// Nothing remembered: once the store is back, an upload works.
			r.store.up()
			mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
		})
	}
}

// F12
func TestStage_Deadline_ReturnsTimeoutAndCleansTemp(t *testing.T) {
	r := newRig(t)
	expired, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := stageBytes(expired, r.vol, "a.png", files.Images, plainPNG(t))
	wantKind(t, err, errs.Timeout)
	r.wantNoObjects(ownerVol)
	r.wantNoTempFiles()
}

func TestStage_Canceled_ReturnsCanceled(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := stageBytes(ctx, r.vol, "a.png", files.Images, plainPNG(t))
	wantKind(t, err, errs.Canceled)
	r.wantNoObjects(ownerVol)
	r.wantNoTempFiles()
}

// cancelMidway cancels the context after the first read, then keeps serving data.
type cancelMidway struct {
	cancel context.CancelFunc
	data   *bytes.Reader
	reads  int
}

func (c *cancelMidway) Read(p []byte) (int, error) {
	c.reads++
	if c.reads == 2 {
		c.cancel()
	}
	if len(p) > 700 {
		p = p[:700] // small reads, so there is a "midway"
	}
	return c.data.Read(p)
}

func TestStage_CanceledWhileStreaming_ReturnsCanceledAndCleansTemp(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	body := &cancelMidway{cancel: cancel, data: bytes.NewReader(pdfOfSize(t, 200<<10))}
	_, err := r.vol.Stage(ctx, files.Upload{Name: "a.pdf", Accept: files.Documents, Body: body})
	wantKind(t, err, errs.Canceled)
	r.wantNoObjects(ownerVol)
	r.wantNoTempFiles()
}

func TestStage_BodyReadFails_ReturnsAnErrorAndCleansUp(t *testing.T) {
	r := newRig(t)
	boom := errors.New("connection reset")
	body := io.MultiReader(bytes.NewReader(pdfDoc("")), &failingReader{err: boom})
	_, err := r.vol.Stage(ctxT(t), files.Upload{Name: "a.pdf", Accept: files.Documents, Body: body})
	if err == nil {
		t.Fatal("Stage succeeded although the body failed midway")
	}
	r.wantNoObjects(ownerVol)
	r.wantNoTempFiles()
}

// A body error that is already a classified *errs.Error comes back unchanged (files.md,
// "Stage"; settings' MaxLogoBytes reader relies on it).
func TestStage_ClassifiedBodyError_ReturnedUnchanged(t *testing.T) {
	r := newRig(t)
	want := errs.E("test.cap", errs.Conflict, errors.New("a classified failure"))
	body := io.MultiReader(bytes.NewReader(pdfDoc("")), &failingReader{err: want})
	_, err := r.vol.Stage(ctxT(t), files.Upload{Name: "a.pdf", Accept: files.Documents, Body: body})
	if !errors.Is(err, want) || !errs.Is(err, errs.Conflict) {
		t.Fatalf("Stage = %v (kind %v), want the body's own error unchanged", err, errs.KindOf(err))
	}
	r.wantNoObjects(ownerVol)
	r.wantNoTempFiles()
}

// ---- Discard ----

func TestDiscard_AfterStage_LeavesNothing(t *testing.T) {
	r := newRig(t, withAttachedLookup(false, nil))
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	if got := len(r.objectKeys(ownerVol)); got != 1 {
		t.Fatalf("objects after Stage = %d", got)
	}
	wantNoErr(t, r.vol.Discard(ctxT(t), st))
	r.wantNoObjects(ownerVol)
	wantNoErr(t, r.vol.Discard(ctxT(t), st)) // idempotent
	wantNoErr(t, r.vol.Discard(ctxT(t), st))
}

// A NotFound from the attached lookup is treated as "attached": the unsafe direction never
// deletes bytes.
func TestDiscard_AttachedLookupNotFound_KeepsObject(t *testing.T) {
	r := newRig(t) // dbtest.NewFake: QueryRow yields NotFound
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	wantNoErr(t, r.vol.Discard(ctxT(t), st))
	if got := len(r.objectKeys(ownerVol)); got != 1 {
		t.Errorf("objects = %d; a NotFound lookup must keep the object", got)
	}
}

// F23
func TestDiscard_DeleteFails_ReturnsUnavailable(t *testing.T) {
	r := newRig(t, withAttachedLookup(false, nil))
	st := mustStage(t, r.vol, "a.png", files.Images, plainPNG(t))
	r.store.fail("delete", unavailable("delete"))
	err := r.vol.Discard(ctxT(t), st)
	wantKind(t, err, errs.Unavailable)
	if got := len(r.objectKeys(ownerVol)); got != 1 {
		t.Errorf("objects = %d; the object must be left for the sweep", got)
	}
	// And the sweep (see the hygiene tests) removes it; a retry once the store is back works.
	r.store.up()
	wantNoErr(t, r.vol.Discard(ctxT(t), st))
	r.wantNoObjects(ownerVol)
}

// Discard never reaches into another owner's bucket.
func TestDiscard_ForeignStaged_DoesNotTouchOtherBucket(t *testing.T) {
	r := newRig(t)
	st := mustStage(t, r.inc, "a.png", files.Images, plainPNG(t))
	_ = r.vol.Discard(ctxT(t), st) //nolint:errcheck // either a no-op or an Invalid is acceptable; the object must survive
	if got := len(r.objectKeys(ownerInc)); got != 1 {
		t.Errorf("incidents' object count = %d; volunteers' Discard removed another owner's object", got)
	}
}

// ---- concurrency ----

func TestStage_Concurrent_DistinctObjectsNoTempLeft(t *testing.T) {
	r := newRig(t)
	const n = 16
	type result struct {
		key objects.Key
		err error
	}
	res := make(chan result, n)
	for i := range n {
		go func() {
			body := pdfDoc(strings.Repeat("%", i+1) + "\n")
			st, err := stageBytes(ctxT(t), r.vol, "c.pdf", files.Documents, body)
			res <- result{key: st.Key, err: err}
		}()
	}
	keys := make([]objects.Key, 0, n)
	for range n {
		got := <-res
		wantNoErr(t, got.err)
		keys = append(keys, got.key)
	}
	seen := map[objects.Key]bool{}
	for _, k := range keys {
		if seen[k] {
			t.Errorf("duplicate key %s", k)
		}
		seen[k] = true
	}
	if len(seen) != n || len(r.objectKeys(ownerVol)) != n {
		t.Errorf("keys = %d, objects = %d, want %d", len(seen), len(r.objectKeys(ownerVol)), n)
	}
	r.wantNoTempFiles()
}
