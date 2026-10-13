package colorfont

import (
	"fmt"
)

// LayerV0 is one layer of a glyph in a COLR version 0 table.
type LayerV0 struct {
	// GlyphID draws this layer.
	GlyphID uint16

	// PaletteIndex selects a CPAL entry.
	PaletteIndex uint16
}

// COLR is a parsed COLR table, in either version.
type COLR struct {
	Version uint16

	// V0Layers maps a base glyph id to its layers. It is populated for version 0
	// fonts and for version 1 fonts that also carry version 0 records, which the
	// format permits.
	V0Layers map[uint16][]LayerV0

	// V1 maps a base glyph id to its paint graph.
	V1 map[uint16]*Paint
}

// Paint is one node of a COLR version 1 paint graph.
type Paint struct {
	// Format is the on-disk format number. It is kept because it is the only
	// honest description of a paint this package does not decode: reporting the
	// number beats inventing a colour for a gradient.
	Format uint8

	// PaletteIndex and Alpha are meaningful for PaintSolid.
	PaletteIndex uint16

	// Alpha is the F2Dot14 value as a float. It is signed on disk.
	Alpha float64

	// GlyphID is set for PaintGlyph.
	GlyphID uint16

	// Paint is the child node for the single-child formats.
	Paint *Paint

	// Layers are the children of PaintColrLayers.
	Layers []*Paint
}

// Paint format numbers, read from fontTools' otTables.Paint converter table
// rather than recalled. An earlier attempt assumed 12 was PaintGlyph; it is
// PaintTransform, and PaintGlyph is 10.
const (
	paintColrLayers = 1
	paintSolid      = 2
	paintVarSolid   = 3
	paintGlyph      = 10
)

// maxPaintDepth bounds recursion. A malformed font can make a paint point at
// itself, and without a limit the reader would recurse until the stack runs out.
const maxPaintDepth = 64

// COLR parses the font's COLR table.
func (f *Font) COLR() (*COLR, error) {
	b, err := f.table("COLR")
	if err != nil {
		return nil, err
	}
	r := newReader(b)
	version := r.u16()
	c := &COLR{Version: version, V0Layers: map[uint16][]LayerV0{}, V1: map[uint16]*Paint{}}

	// The header is offset-based and the offsets are 32 bits wide. A flat
	// reading of the first bytes as a run of uint16 yields a base glyph of gid 20
	// with one layer and a layer count of zero -- all of it wrong, and all of it
	// plausible enough to pass a careless check.
	numBaseGlyphRecords := r.u16()
	baseGlyphRecordsOffset := r.u32()
	layerRecordsOffset := r.u32()
	numLayerRecords := r.u16()
	if err := r.err; err != nil {
		return nil, err
	}

	if numBaseGlyphRecords > 0 {
		if err := c.readV0(r, numBaseGlyphRecords, baseGlyphRecordsOffset, numLayerRecords, layerRecordsOffset); err != nil {
			return nil, err
		}
	}

	if version < 1 {
		return c, nil
	}

	baseGlyphListOffset := r.u32()
	layerListOffset := r.u32()
	_ = r.u32() // clipList
	_ = r.u32() // varIndexMap
	_ = r.u32() // varStore
	if err := r.err; err != nil {
		return nil, err
	}
	if baseGlyphListOffset == 0 {
		return c, nil
	}

	ll, err := readLayerList(r, layerListOffset)
	if err != nil {
		return nil, err
	}
	return c, c.readV1(r, baseGlyphListOffset, ll)
}

// readV0 parses the version 0 base glyph and layer records.
func (c *COLR) readV0(r *reader, numBase uint16, baseOff uint32, numLayer uint16, layerOff uint32) error {
	base := r.at(baseOff)

	for i := 0; i < int(numBase); i++ {
		glyph := base.u16()
		first := base.u16()
		count := base.u16()
		if err := base.err; err != nil {
			return fmt.Errorf("COLR v0 base glyph record %d: %w", i, err)
		}
		// Layer records form one array; a base record names a slice of it, which
		// is why first is an index rather than an offset. Bounds are checked
		// before the slice is used.
		if int(first)+int(count) > int(numLayer) {
			return fmt.Errorf("%w: COLR v0 base glyph %d wants layers %d..%d of %d",
				ErrMalformed, glyph, first, int(first)+int(count), numLayer)
		}
		lr := r.at(layerOff + uint32(first)*4)
		ls := make([]LayerV0, 0, count)
		for j := 0; j < int(count); j++ {
			g := lr.u16()
			p := lr.u16()
			if err := lr.err; err != nil {
				return fmt.Errorf("COLR v0 layer for glyph %d: %w", glyph, err)
			}
			ls = append(ls, LayerV0{GlyphID: g, PaletteIndex: p})
		}
		c.V0Layers[glyph] = ls
	}
	return nil
}

// layerList is a parsed COLR v1 LayerList.
//
// The LayerList records the absolute location and the offset array together,
// because a paint offset in that array is relative to the start of the
// LayerList, while a paint's own child reference is relative to the paint. Mixing
// those two bases produces a successful-looking parse of the wrong bytes.
type layerList struct {
	data    []byte
	offsets []uint32
}

// readLayerList parses the LayerList at the given absolute offset.
func readLayerList(r *reader, off uint32) (*layerList, error) {
	if off == 0 {
		return nil, nil
	}
	ll := r.at(off)
	// Paint offsets in the LayerList are relative to the start of the LayerList,
	// so the region kept for them begins here, before the count and the offset
	// array are consumed. Taking the region after those fields makes every
	// offset off by the header size and the parse fails on a valid font -- which
	// is exactly what happened first.
	llStart := ll.off
	count := ll.u32()
	if err := ll.err; err != nil {
		return nil, fmt.Errorf("COLR v1 LayerList: %w", err)
	}
	if int(count) > ll.remaining()/4 {
		return nil, fmt.Errorf("%w: COLR v1 LayerList claims %d paints in %d bytes",
			ErrMalformed, count, ll.remaining())
	}
	offsets := make([]uint32, 0, count)
	for i := 0; i < int(count); i++ {
		offsets = append(offsets, ll.u32())
	}
	if err := ll.err; err != nil {
		return nil, fmt.Errorf("COLR v1 LayerList offsets: %w", err)
	}

	data := make([]byte, 0, len(ll.b)-llStart)
	data = append(data, ll.b[llStart:]...)
	return &layerList{data: data, offsets: offsets}, nil
}

// readV1 parses the BaseGlyphList and each glyph's paint graph.
func (c *COLR) readV1(r *reader, baseGlyphListOff uint32, ll *layerList) error {
	bgl := r.at(baseGlyphListOff)
	// Remember where the BaseGlyphList starts: a paint offset in one of its
	// records is relative to this point, and the cursor moves past it while the
	// records are read.
	bglStart := bgl.off
	count := bgl.u32()
	if err := bgl.err; err != nil {
		return fmt.Errorf("COLR v1 BaseGlyphList: %w", err)
	}
	if int(count) > bgl.remaining()/6 {
		return fmt.Errorf("%w: COLR v1 BaseGlyphList claims %d records in %d bytes",
			ErrMalformed, count, bgl.remaining())
	}

	type rec struct {
		glyph  uint16
		offset uint32
	}
	recs := make([]rec, 0, count)
	for i := 0; i < int(count); i++ {
		g := bgl.u16()
		o := bgl.u32()
		if err := bgl.err; err != nil {
			return fmt.Errorf("COLR v1 base glyph record %d: %w", i, err)
		}
		recs = append(recs, rec{glyph: g, offset: o})
	}

	// A paint offset inside a BaseGlyphPaintRecord is relative to the start of
	// the BaseGlyphList, so the region handed to the paint reader begins at the
	// BaseGlyphList itself.
	// The region a paint offset is measured from starts at the BaseGlyphList and
	// runs to the end of the table: a paint graph may sit after the LayerList,
	// which itself sits outside the BaseGlyphList. Slicing to the end of the
	// BaseGlyphList instead would cut the graph off.
	region := bgl.b[bglStart:]
	for _, rc := range recs {
		p, err := c.readPaint(region, int(rc.offset), ll, 0)
		if err != nil {
			return fmt.Errorf("COLR v1 glyph %d paint: %w", rc.glyph, err)
		}
		c.V1[rc.glyph] = p
	}
	return nil
}

// readPaint decodes one paint node from region, which is the byte slice the
// node's offsets are relative to.
func (c *COLR) readPaint(region []byte, abs int, ll *layerList, depth int) (*Paint, error) {
	if depth > maxPaintDepth {
		return nil, fmt.Errorf("%w: COLR paint graph nests deeper than %d", ErrMalformed, maxPaintDepth)
	}
	if abs < 0 || abs >= len(region) {
		return nil, fmt.Errorf("%w: COLR paint offset %d is outside a %d-byte region",
			ErrMalformed, abs, len(region))
	}
	r := &reader{b: region, off: abs}
	format := r.u8()
	if err := r.err; err != nil {
		return nil, err
	}
	p := &Paint{Format: format}

	switch format {
	case paintColrLayers:
		// NumLayers(u8) FirstLayerIndex(u32).
		n := r.u8()
		first := r.u32()
		if err := r.err; err != nil {
			return nil, err
		}
		if ll == nil {
			return nil, fmt.Errorf("%w: PaintColrLayers with no LayerList", ErrMalformed)
		}
		for i := 0; i < int(n); i++ {
			idx := int(first) + i
			if idx >= len(ll.offsets) {
				return nil, fmt.Errorf("%w: PaintColrLayers wants layer %d of %d",
					ErrMalformed, idx, len(ll.offsets))
			}
			// A LayerList paint offset is relative to the start of the
			// LayerList, which is exactly what ll.data is.
			child, err := c.readPaint(ll.data, int(ll.offsets[idx]), ll, depth+1)
			if err != nil {
				return nil, err
			}
			p.Layers = append(p.Layers, child)
		}

	case paintSolid, paintVarSolid:
		// PaletteIndex(u16) Alpha(F2Dot14). Alpha is signed: reading it as
		// unsigned turns a valid -0.5 into 1.5.
		p.PaletteIndex = r.u16()
		p.Alpha = float64(r.i16()) / 16384.0
		if format == paintVarSolid {
			_ = r.u32() // VarIndexBase, not resolved without the variation store
		}

	case paintGlyph:
		// Paint is an Offset24, three bytes. Reading four consumes a byte of the
		// glyph id: gid 2 becomes 514, which was a real misparse here.
		childOff := r.u24()
		p.GlyphID = r.u16()
		if err := r.err; err != nil {
			return nil, err
		}
		// The child reference is relative to this paint record, whose absolute
		// position is abs.
		child, err := c.readPaint(region, abs+int(childOff), ll, depth+1)
		if err != nil {
			return nil, err
		}
		p.Paint = child

	default:
		// Recorded by number and not decoded. Returning the number is honest;
		// picking a colour for a gradient would not be.
		return p, nil
	}
	return p, nil
}
