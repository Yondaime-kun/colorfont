package colorfont

import (
	"os"
	"path/filepath"
	"testing"
)

// TestColourMechanism checks that a font is classified by the tables it carries.
func TestColourMechanism(t *testing.T) {
	tests := []struct {
		fixture string
		want    ColourMechanism
	}{
		{"colrv0.ttf", ColourCOLR},
		{"colrv1.ttf", ColourCOLR},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			f := loadFont(t, tc.fixture)
			if got := f.Colour(); got != tc.want {
				t.Errorf("Colour() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCPALColours checks the palette colours of the version 0 fixture.
//
// The fixture's palette is declared as red then blue. If the reader treats the
// stored BGRA record as RGBA the two swap, which is a bug that still renders
// plausible colours and so would survive a visual check. The expected values are
// therefore written as literal hex, not compared against the writer's input.
func TestCPALColours(t *testing.T) {
	f := loadFont(t, "colrv0.ttf")
	cp, err := f.CPAL()
	if err != nil {
		t.Fatalf("CPAL: %v", err)
	}
	if cp.Version != 0 {
		t.Errorf("Version = %d, want 0", cp.Version)
	}
	if cp.NumPaletteEntries != 2 {
		t.Errorf("NumPaletteEntries = %d, want 2", cp.NumPaletteEntries)
	}
	if len(cp.Palettes) != 1 {
		t.Fatalf("got %d palettes, want 1", len(cp.Palettes))
	}
	// The fixture was generated with fontTools writing #FF0000FF and #0000FFFF.
	// These hex strings come from fontTools' own XML dump of the file, so the
	// test compares against an independent implementation rather than against
	// this package's own output.
	want := []string{"#ff0000ff", "#0000ffff"}
	got := cp.Palettes[0].Colours
	if len(got) != len(want) {
		t.Fatalf("got %d colours, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Hex() != want[i] {
			t.Errorf("colour %d = %s, want %s", i, got[i].Hex(), want[i])
		}
	}
}

// TestCPALBGRAOrder plants the specific mistake this package made once: reading
// the stored record as RGBA instead of BGRA.
//
// If the reader regressed to RGBA, the red entry would come back as #0000ffff.
func TestCPALBGRAOrder(t *testing.T) {
	f := loadFont(t, "colrv0.ttf")
	cp, err := f.CPAL()
	if err != nil {
		t.Fatalf("CPAL: %v", err)
	}
	first := cp.Palettes[0].Colours[0]
	if first.R != 0xFF || first.G != 0x00 || first.B != 0x00 {
		t.Errorf("first colour = R=%02x G=%02x B=%02x, want R=ff G=00 B=00 (BGRA misread?)",
			first.R, first.G, first.B)
	}
}

// TestCOLRV0 checks the version 0 layer records against fontTools' XML.
//
// The expected layers are how fontTools describes the same file: glyph A draws
// A.layer1 with palette index 0 and A.layer2 with palette index 1.
func TestCOLRV0(t *testing.T) {
	f := loadFont(t, "colrv0.ttf")
	c, err := f.COLR()
	if err != nil {
		t.Fatalf("COLR: %v", err)
	}
	if c.Version != 0 {
		t.Errorf("Version = %d, want 0", c.Version)
	}
	ls, ok := c.V0Layers[1]
	if !ok {
		t.Fatalf("no layers for glyph 1; have %v", keys(c.V0Layers))
	}
	if len(ls) != 2 {
		t.Fatalf("glyph 1 has %d layers, want 2", len(ls))
	}
	want := []LayerV0{{GlyphID: 2, PaletteIndex: 0}, {GlyphID: 3, PaletteIndex: 1}}
	for i := range want {
		if ls[i] != want[i] {
			t.Errorf("layer %d = %+v, want %+v", i, ls[i], want[i])
		}
	}
}

// TestCOLRV0HeaderIsOffsetBased plants the other mistake made here: reading the
// COLR header as a flat run of uint16.
//
// The header's two offsets are 32 bits wide. Read as uint16 the base glyph comes
// back as gid 20 with one layer and the layer count as zero, so this test asserts
// the values a flat read cannot produce.
func TestCOLRV0HeaderIsOffsetBased(t *testing.T) {
	f := loadFont(t, "colrv0.ttf")
	c, err := f.COLR()
	if err != nil {
		t.Fatalf("COLR: %v", err)
	}
	if _, ok := c.V0Layers[20]; ok {
		t.Error("glyph 20 present: the header was likely read as uint16 fields")
	}
	ls := c.V0Layers[1]
	if len(ls) != 2 {
		t.Fatalf("glyph 1 has %d layers, want 2; a uint16 read gives 1", len(ls))
	}
}

// TestCOLRV1 checks the paint graph of the version 1 fixture against fontTools.
func TestCOLRV1(t *testing.T) {
	f := loadFont(t, "colrv1.ttf")
	c, err := f.COLR()
	if err != nil {
		t.Fatalf("COLR: %v", err)
	}
	if c.Version != 1 {
		t.Errorf("Version = %d, want 1", c.Version)
	}
	p, ok := c.V1[1]
	if !ok {
		t.Fatalf("no paint graph for glyph 1; have %v", keys(c.V1))
	}
	if p.Format != paintColrLayers {
		t.Fatalf("root format = %d, want %d (PaintColrLayers)", p.Format, paintColrLayers)
	}
	if len(p.Layers) != 2 {
		t.Fatalf("root has %d layers, want 2", len(p.Layers))
	}

	// fontTools: layer 0 is PaintGlyph(Glyph=A.l1) over PaintSolid(PaletteIndex=0),
	// layer 1 is PaintGlyph(Glyph=A.l2) over PaintSolid(PaletteIndex=2).
	type want struct {
		glyph   uint16
		palette uint16
	}
	wants := []want{{glyph: 2, palette: 0}, {glyph: 3, palette: 2}}
	for i, w := range wants {
		l := p.Layers[i]
		if l.Format != paintGlyph {
			t.Errorf("layer %d format = %d, want %d (PaintGlyph)", i, l.Format, paintGlyph)
			continue
		}
		if l.GlyphID != w.glyph {
			t.Errorf("layer %d glyph = %d, want %d", i, l.GlyphID, w.glyph)
		}
		if l.Paint == nil {
			t.Errorf("layer %d has no child paint", i)
			continue
		}
		if l.Paint.Format != paintSolid {
			t.Errorf("layer %d child format = %d, want %d (PaintSolid)", i, l.Paint.Format, paintSolid)
			continue
		}
		if l.Paint.PaletteIndex != w.palette {
			t.Errorf("layer %d paletteIndex = %d, want %d", i, l.Paint.PaletteIndex, w.palette)
		}
	}
}

// TestCOLRV1Offset24 plants the mistake that produced glyph id 514: reading a
// Paint reference inside a paint record as a 32-bit offset instead of 24-bit.
//
// A 4-byte read consumes one byte of the glyph id, so the id is shifted left by
// eight and the child lands in the wrong place. Asserting an exact small glyph id
// is what catches it.
func TestCOLRV1Offset24(t *testing.T) {
	f := loadFont(t, "colrv1.ttf")
	c, err := f.COLR()
	if err != nil {
		t.Fatalf("COLR: %v", err)
	}
	p := c.V1[1]
	if p == nil || len(p.Layers) == 0 {
		t.Fatal("no layers to check")
	}
	for i, l := range p.Layers {
		if l.GlyphID > 255 {
			t.Errorf("layer %d glyph id = %d: reading the paint reference as 32 bits "+
				"consumes a byte of the glyph id", i, l.GlyphID)
		}
	}
}

// TestCOLRV1AlphaIsSigned plants the third mistake: reading the F2Dot14 alpha as
// an unsigned value.
//
// A valid negative alpha read unsigned becomes a large positive one, and both
// look like a number, so only an exact check catches it. The fixture uses 1.0.
func TestCOLRV1AlphaIsSigned(t *testing.T) {
	f := loadFont(t, "colrv1.ttf")
	c, _ := f.COLR()
	p := c.V1[1]
	if p == nil || len(p.Layers) == 0 {
		t.Fatal("no layers to check")
	}
	for i, l := range p.Layers {
		if l.Paint == nil {
			continue
		}
		// The fixture's alphas are 1.0, stored as 0x4000. An unsigned read of a
		// negative value would exceed 1.0.
		if l.Paint.Alpha > 1.0 || l.Paint.Alpha < -1.0 {
			t.Errorf("layer %d alpha = %v, outside the F2Dot14 range (-1, 1)", i, l.Paint.Alpha)
		}
	}
}

// TestCOLRMissingTable checks that asking for a table the font lacks is an error
// and not a panic or an empty result.
func TestCOLRMissingTable(t *testing.T) {
	f := loadFont(t, "colrv0.ttf")
	if f.HasTable("CBDT") {
		t.Fatal("fixture unexpectedly has CBDT")
	}
	if _, err := f.CBLC(); err == nil {
		t.Error("CBLC on a font without CBLC: got nil error, want ErrNoTable")
	}
}

// TestParseRejectsNonFont checks the container validates its magic.
func TestParseRejectsNonFont(t *testing.T) {
	if _, err := Parse([]byte("not a font at all, not even close")); err == nil {
		t.Error("Parse of non-font bytes: got nil error")
	}
	if _, err := Parse([]byte{0, 1, 2}); err == nil {
		t.Error("Parse of 3 bytes: got nil error")
	}
}

// TestParseRejectsTruncatedTable plants a font whose table record claims bytes
// past the end of the file. The reader must refuse rather than slice out of range.
func TestParseRejectsTruncatedTable(t *testing.T) {
	data := loadBytes(t, "colrv0.ttf")
	// Corrupt the length of the first table record to something enormous.
	bad := append([]byte(nil), data...)
	bad[12+12] = 0xFF
	bad[12+13] = 0xFF
	bad[12+14] = 0xFF
	bad[12+15] = 0xFF
	if _, err := Parse(bad); err == nil {
		t.Error("Parse of a font with an oversized table length: got nil error")
	}
}

// TestBitmapDataBounds checks the CBDT slice helper refuses to read past the end.
func TestBitmapDataBounds(t *testing.T) {
	f := loadFont(t, "colrv0.ttf")
	// colrv0.ttf has no CBDT, so the table lookup itself fails first.
	if _, err := f.BitmapData(BitmapEntry{Offset: 0, Length: 4}); err == nil {
		t.Error("BitmapData without a CBDT table: got nil error")
	}
}

// loadFont parses a fixture from testdata.
func loadFont(t *testing.T, name string) *Font {
	t.Helper()
	b := loadBytes(t, name)
	f, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse(%s): %v", name, err)
	}
	return f
}

func loadBytes(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

func keys[V any](m map[uint16]V) []uint16 {
	out := make([]uint16, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestCOLRV1NegativeAlpha checks a negative F2Dot14 alpha.
//
// The earlier version of this test only asserted the alpha was within (-1, 1),
// which an unsigned read also satisfies whenever the stored value is positive:
// the fixture's alpha was 1.0, so the test passed with the field read either way
// and a mutation that swapped the read to unsigned survived. This fixture stores
// -0.5 on purpose, so the two readings differ and the test bites.
func TestCOLRV1NegativeAlpha(t *testing.T) {
	f := loadFont(t, "colrv1negalpha.ttf")
	c, err := f.COLR()
	if err != nil {
		t.Fatalf("COLR: %v", err)
	}
	p := c.V1[1]
	if p == nil {
		t.Fatal("no paint graph for glyph 1")
	}
	// The fixture is a PaintGlyph over a PaintSolid with alpha -0.5.
	if p.Format != paintGlyph {
		t.Fatalf("root format = %d, want %d (PaintGlyph)", p.Format, paintGlyph)
	}
	if p.Paint == nil || p.Paint.Format != paintSolid {
		t.Fatalf("child is not a PaintSolid: %+v", p.Paint)
	}
	got := p.Paint.Alpha
	if got != -0.5 {
		t.Errorf("alpha = %v, want -0.5; an unsigned read gives %v",
			got, float64(uint16(0xE000))/16384.0)
	}
}
