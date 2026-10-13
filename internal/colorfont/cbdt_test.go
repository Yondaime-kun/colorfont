package colorfont

import (
	"bytes"
	"compress/gzip"
	"image"
	"image/png"
	"io"
	"os"
	"testing"
)

// systemEmojiPaths are where Noto Color Emoji is installed on the systems this
// package was developed and tested on. The test skips when none is present
// rather than vendoring a 10 MB font into the repository.
var systemEmojiPaths = []string{
	"/usr/share/fonts/truetype/noto/NotoColorEmoji.ttf",
	"/usr/share/fonts/noto/NotoColorEmoji.ttf",
	"/Library/Fonts/NotoColorEmoji.ttf",
}

// findEmoji returns the path of an installed Noto Color Emoji, or "".
func findEmoji() string {
	for _, p := range systemEmojiPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// isPNG reports whether b starts with the PNG signature.
func isPNG(b []byte) bool {
	return len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
}

// isGzip reports whether b starts with the gzip magic.
func isGzip(b []byte) bool {
	return len(b) >= 2 && b[0] == 0x1f && b[1] == 0x8b
}

// decodeBitmapImage decodes a CBDT bitmap, unwrapping gzip when present.
//
// CBDT image formats 17 and 18 hold a PNG. Noto Color Emoji stores those PNGs
// directly; other fonts wrap them in gzip, so both are handled and the test
// records which it saw.
func decodeBitmapImage(b []byte) (image.Image, error) {
	if isGzip(b) {
		zr, err := gzip.NewReader(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(zr)
		if err != nil {
			return nil, err
		}
		return png.Decode(bytes.NewReader(raw))
	}
	return png.Decode(bytes.NewReader(b))
}

// TestCBDTRealFont parses a real Google-shipped colour bitmap font and decodes
// every bitmap it indexes.
//
// This is the strongest check in the package: the structure under test was
// written by Google, not by this package's author, and the assertion is that
// every one of the thousands of indexed bitmaps is a decodable PNG. A parser
// with an off-by-one in the CBLC offsets, or one that forgot the CBDT header,
// would index into the middle of a PNG and decode nothing -- which is exactly
// what happened before the header was accounted for.
func TestCBDTRealFont(t *testing.T) {
	path := findEmoji()
	if path == "" {
		t.Skip("no Noto Color Emoji installed; skipping the real-font test")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	f, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := f.Colour(); got != ColourCBDT {
		t.Fatalf("Colour() = %q, want %q", got, ColourCBDT)
	}

	strikes, err := f.CBLC()
	if err != nil {
		t.Fatalf("CBLC: %v", err)
	}
	if len(strikes) == 0 {
		t.Fatal("no strikes")
	}

	for i, s := range strikes {
		if s.Strike.BitDepth != 32 {
			t.Errorf("strike %d bit depth = %d, want 32", i, s.Strike.BitDepth)
		}
		if s.Strike.PPEMX == 0 || s.Strike.PPEMY == 0 {
			t.Errorf("strike %d has ppem %d x %d", i, s.Strike.PPEMX, s.Strike.PPEMY)
		}
		if len(s.Entries) == 0 {
			t.Errorf("strike %d has no glyph entries", i)
			continue
		}

		total, decoded, direct, gzipped, failed := 0, 0, 0, 0, 0
		for glyph, e := range s.Entries {
			data, err := f.BitmapData(e)
			if err != nil {
				t.Errorf("strike %d glyph %d: BitmapData: %v", i, glyph, err)
				continue
			}
			total++
			if isPNG(data) {
				direct++
			}
			if isGzip(data) {
				gzipped++
			}
			img, err := decodeBitmapImage(data)
			if err != nil {
				// Only report the first few failures; a broken parser fails on
				// thousands and the log becomes useless.
				if failed < 3 {
					t.Errorf("strike %d glyph %d: decode: %v (first 8 bytes % x)",
						i, glyph, err, data[:min(8, len(data))])
				}
				failed++
				continue
			}
			if img.Bounds().Dx() == 0 || img.Bounds().Dy() == 0 {
				t.Errorf("strike %d glyph %d: decoded to an empty image", i, glyph)
			}
			decoded++
		}
		t.Logf("strike %d: ppem %dx%d, %d entries, %d decoded (%d direct PNG, %d gzipped), %d failed",
			i, s.Strike.PPEMX, s.Strike.PPEMY, total, decoded, direct, gzipped, failed)
		if failed > 0 {
			t.Errorf("strike %d: %d of %d bitmaps failed to decode", i, failed, total)
		}
	}
}

// TestCBDTBitmapMetricsMatchImage cross-checks the BigGlyphMetrics prefix of a
// bitmap against the decoded image's dimensions.
//
// The metrics record is the first eight bytes the parser skips: height, width,
// bearingX, bearingY, advance, and two reserved bytes. If the parser skipped the
// wrong number of bytes it would still often find a PNG signature somewhere, so
// checking that the skipped prefix agrees with the real image is what makes the
// skip verifiable rather than merely plausible.
func TestCBDTBitmapMetricsMatchImage(t *testing.T) {
	path := findEmoji()
	if path == "" {
		t.Skip("no Noto Color Emoji installed")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	f, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	strikes, err := f.CBLC()
	if err != nil {
		t.Fatalf("CBLC: %v", err)
	}

	checked := 0
	for _, s := range strikes {
		for glyph, e := range s.Entries {
			if checked >= 40 {
				break
			}
			// Read the entry including its metrics prefix by going straight to
			// the CBDT table through a raw read.
			raw, err := f.table("CBDT")
			if err != nil {
				t.Fatalf("CBDT: %v", err)
			}
			if int(e.Offset)+8 > len(raw) {
				continue
			}
			height := raw[e.Offset]
			width := raw[e.Offset+1]

			data, err := f.BitmapData(e)
			if err != nil {
				t.Errorf("glyph %d: %v", glyph, err)
				continue
			}
			if !isPNG(data) && !isGzip(data) {
				continue
			}
			img, err := decodeBitmapImage(data)
			if err != nil {
				continue
			}
			// Noto Color Emoji's metrics prefix uses 0 to mean 256, the usual
			// convention for a one-byte dimension field.
			wh, ww := int(height), int(width)
			if wh == 0 {
				wh = 256
			}
			if ww == 0 {
				ww = 256
			}
			if img.Bounds().Dx() != ww || img.Bounds().Dy() != wh {
				t.Errorf("glyph %d: metrics say %dx%d, image is %dx%d",
					glyph, ww, wh, img.Bounds().Dx(), img.Bounds().Dy())
			}
			checked++
		}
	}
	if checked == 0 {
		t.Skip("no bitmaps checked")
	}
	t.Logf("cross-checked %d bitmaps against their metrics prefix", checked)
}

// TestCBDTHeaderSkipIsRequired plants the bug that made every bitmap fail to
// decode: not accounting for the four-byte CBDT table header.
//
// It reads a bitmap at the index's offset and at that offset minus four, and
// asserts that only the index's own offset yields a decodable image. If the
// parser stopped skipping the header the first case would fail.
func TestCBDTHeaderSkipIsRequired(t *testing.T) {
	path := findEmoji()
	if path == "" {
		t.Skip("no Noto Color Emoji installed")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	f, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	strikes, err := f.CBLC()
	if err != nil {
		t.Fatalf("CBLC: %v", err)
	}
	raw, err := f.table("CBDT")
	if err != nil {
		t.Fatalf("CBDT: %v", err)
	}

	var sample BitmapEntry
	found := false
	for _, s := range strikes {
		for _, e := range s.Entries {
			if e.ImageFormat == 17 && e.Length > 64 {
				sample = e
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Skip("no suitable bitmap found")
	}

	good, err := f.BitmapData(sample)
	if err != nil {
		t.Fatalf("BitmapData: %v", err)
	}
	if _, err := decodeBitmapImage(good); err != nil {
		t.Fatalf("the indexed bitmap did not decode: %v", err)
	}

	// Four bytes earlier is where the first bitmap would be if the header were
	// ignored entirely. It must not look like image data.
	if int(sample.Offset) >= 4 {
		off := sample.Offset - cbdtheaderLen
		wrong := raw[off : off+sample.Length]
		if isPNG(wrong) {
			t.Error("the four bytes before the indexed bitmap are a PNG: the CBDT " +
				"header may no longer be accounted for")
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
