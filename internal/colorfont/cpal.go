package colorfont

import (
	"fmt"
)

// Palette is one CPAL palette: a list of colours, plus the optional labels the
// font attaches to it.
type Palette struct {
	// Colours is the palette's entries in order.
	Colours []RGBA

	// Name is the palette's own name, if the font labels it.
	Name string

	// EntryNames are per-entry names, which most fonts leave empty.
	EntryNames []string
}

// RGBA is a colour with straight (non-premultiplied) alpha, in the order the
// name suggests.
//
// The font stores colours as B, G, R, A. Converting here, once, is deliberate:
// a caller that has to remember the on-disk byte order will eventually get it
// wrong, and swapping red and blue still looks like a colour.
type RGBA struct {
	R, G, B, A uint8
}

// Hex renders the colour as #rrggbbaa.
func (c RGBA) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x%02x", c.R, c.G, c.B, c.A)
}

// CPAL is a parsed CPAL table.
type CPAL struct {
	Version           uint16
	NumPaletteEntries uint16
	Palettes          []Palette
}

// CPAL parses the font's CPAL table.
func (f *Font) CPAL() (*CPAL, error) {
	b, err := f.table("CPAL")
	if err != nil {
		return nil, err
	}
	r := newReader(b)

	version := r.u16()
	numEntries := r.u16()
	numPalettes := r.u16()
	numColorRecords := r.u16()
	offsetFirstColorRecord := r.u32()

	// Bounds first: the counts are attacker-controlled and every one of them is
	// used to size a loop or an allocation below.
	if numPalettes > 0 && int(numPalettes) > r.remaining()/2+1 {
		return nil, fmt.Errorf("%w: CPAL claims %d palettes in a %d-byte table",
			ErrMalformed, numPalettes, len(b))
	}
	if int(numEntries)*int(numPalettes) > len(b) {
		return nil, fmt.Errorf("%w: CPAL claims %d palette entries in a %d-byte table",
			ErrMalformed, int(numEntries)*int(numPalettes), len(b))
	}

	cp := &CPAL{
		Version:           version,
		NumPaletteEntries: numEntries,
	}

	// Colour records are 4 bytes each and live at offsetFirstColorRecord.
	colorRecs := r.at(offsetFirstColorRecord)
	colours := make([]RGBA, 0, numColorRecords)
	for i := 0; i < int(numColorRecords); i++ {
		// Stored as B, G, R, A. Reading as R, G, B, A swaps red and blue, which
		// was the first bug this package had.
		bl := colorRecs.u8()
		g := colorRecs.u8()
		rd := colorRecs.u8()
		a := colorRecs.u8()
		if colorRecs.err != nil {
			break
		}
		colours = append(colours, RGBA{R: rd, G: g, B: bl, A: a})
	}
	if err := colorRecs.err; err != nil {
		return nil, fmt.Errorf("CPAL colour records: %w", err)
	}

	// After the fixed header come the palette entry indices, one per palette.
	// They are present only when NumPalettes is greater than one; a font with a
	// single palette omits them and every entry is implicitly used in order.
	indices := make([]uint16, 0, numPalettes*numEntries)
	if numPalettes > 1 {
		for i := 0; i < int(numPalettes)*int(numEntries); i++ {
			indices = append(indices, r.u16())
		}
		if err := r.err; err != nil {
			return nil, fmt.Errorf("CPAL palette indices: %w", err)
		}
	}

	// Palette labels and entry labels follow, as arrays of name ids. They are
	// read only if present; their absence is normal.
	var paletteLabels, entryLabels []uint16
	if version >= 1 {
		for i := 0; i < int(numPalettes); i++ {
			paletteLabels = append(paletteLabels, r.u16())
		}
		for i := 0; i < int(numEntries); i++ {
			entryLabels = append(entryLabels, r.u16())
		}
		if err := r.err; err != nil {
			// Labels are optional decoration. A font that truncates here still
			// has usable colours, so this is not fatal.
			paletteLabels, entryLabels = nil, nil
		}
	}

	cp.Palettes = make([]Palette, 0, numPalettes)
	for p := 0; p < int(numPalettes); p++ {
		pal := Palette{Colours: make([]RGBA, 0, numEntries)}
		for e := 0; e < int(numEntries); e++ {
			// With one palette the colours are used in order; with several, the
			// index array says which colour record each entry refers to.
			idx := e
			if len(indices) > p*int(numEntries)+e {
				idx = int(indices[p*int(numEntries)+e])
			}
			if idx < 0 || idx >= len(colours) {
				return nil, fmt.Errorf("%w: CPAL palette %d entry %d refers to colour record %d of %d",
					ErrMalformed, p, e, idx, len(colours))
			}
			pal.Colours = append(pal.Colours, colours[idx])
		}
		cp.Palettes = append(cp.Palettes, pal)
	}

	return cp, nil
}

// LabelNameIDs returns the name ids CPAL uses for palette and entry labels.
//
// They are exposed as ids rather than resolved strings because resolving them
// needs the `name` table, which this package does not parse: a label that cannot
// be shown is better reported as an id than silently dropped.
func (f *Font) CPALLabelNameIDs() (paletteLabels, entryLabels []uint16, err error) {
	b, err := f.table("CPAL")
	if err != nil {
		return nil, nil, err
	}
	r := newReader(b)
	version := r.u16()
	numEntries := r.u16()
	numPalettes := r.u16()
	numColorRecords := r.u16()
	_ = r.u32() // offset to colour records, not needed here

	if version < 1 {
		return nil, nil, nil
	}
	if numPalettes > 1 {
		r.skip(int(numPalettes) * int(numEntries) * 2)
	}
	for i := 0; i < int(numPalettes); i++ {
		paletteLabels = append(paletteLabels, r.u16())
	}
	for i := 0; i < int(numEntries); i++ {
		entryLabels = append(entryLabels, r.u16())
	}
	_ = numColorRecords
	if r.err != nil {
		return nil, nil, r.err
	}
	return paletteLabels, entryLabels, nil
}
