package colorfont

import (
	"fmt"
)

// Strike is one bitmap size in a CBDT/CBLC font.
//
// A font carries several strikes at different pixel sizes, and each strike has
// its own metrics and bit depth. A reader that treats CBLC as a single flat
// table gets nothing useful out of it, because the interesting content is behind
// two levels of index.
type Strike struct {
	// PPEMX and PPEMY are the pixels-per-em the strike was drawn at. A strike is
	// resolution-specific: the same glyph has different bitmaps at 32 and 64.
	PPEMX, PPEMY uint8

	// BitDepth is the colour depth in bits per pixel: 32 for BGRA, 8 for
	// grayscale, 1 for monochrome.
	BitDepth uint8

	// Flags carries the horizontal and vertical orientation bits.
	Flags uint8

	// StartGlyph and EndGlyph bound the glyphs this strike covers.
	StartGlyph, EndGlyph uint16

	// SubtableCount is how many index subtables the strike's array holds.
	SubtableCount uint32
}

// BitmapIndex is the glyph coverage of one strike, resolved from the three-level
// index.
type BitmapIndex struct {
	// Strike is the strike these entries belong to.
	Strike Strike

	// Entries maps a glyph id to the offset and length of its bitmap in the CBDT
	// table.
	Entries map[uint16]BitmapEntry
}

// BitmapEntry locates one glyph's bitmap inside the CBDT table.
type BitmapEntry struct {
	// Offset is where the bitmap data starts, relative to the CBDT table.
	Offset uint32

	// Length is how many bytes it occupies. It is derived from the next entry's
	// offset when the format stores offsets rather than lengths.
	Length uint32

	// ImageFormat is the CBLC image format code for this entry: 17 and 18 are
	// the small and large PNG formats, 1 and 2 are the byte-aligned and bit
	// aligned monochrome formats.
	ImageFormat uint16
}

// CBLC parses the font's CBLC table into a per-strike glyph index.
//
// The structure is three levels deep and each level's offsets are relative to
// something different, which is the whole difficulty of this table:
//
//	CBLC header -> BitmapSize records (one per strike)
//	  BitmapSize -> IndexSubtableArray at indexSubtableArrayOffset
//	    IndexSubtableArray -> IndexSubtable records at arrayOffset+additionalOffset
//	      IndexSubtable -> per-glyph offsets into the CBDT table
func (f *Font) CBLC() ([]BitmapIndex, error) {
	b, err := f.table("CBLC")
	if err != nil {
		return nil, err
	}
	r := newReader(b)

	major := r.u16()
	minor := r.u16()
	numSizes := r.u32()
	if err := r.err; err != nil {
		return nil, fmt.Errorf("CBLC header: %w", err)
	}
	// 48 bytes per BitmapSize record; bound the count before looping.
	if numSizes > 0 && int(numSizes) > (len(b)-8)/48 {
		return nil, fmt.Errorf("%w: CBLC claims %d strikes in a %d-byte table",
			ErrMalformed, numSizes, len(b))
	}
	_ = major
	_ = minor

	out := make([]BitmapIndex, 0, numSizes)
	for i := 0; i < int(numSizes); i++ {
		st, arrayOffset, err := readBitmapSize(r)
		if err != nil {
			return nil, fmt.Errorf("CBLC strike %d: %w", i, err)
		}
		idx := BitmapIndex{Strike: st, Entries: map[uint16]BitmapEntry{}}
		if err := readIndexSubtables(r, st, arrayOffset, &idx); err != nil {
			return nil, fmt.Errorf("CBLC strike %d subtables: %w", i, err)
		}
		out = append(out, idx)
	}
	return out, nil
}

// readBitmapSize reads one 48-byte BitmapSize record.
func readBitmapSize(r *reader) (Strike, uint32, error) {
	var s Strike
	arrayOffset := r.u32()
	_ = r.u32() // indexTablesSize
	s.SubtableCount = r.u32()
	_ = r.u32() // colorRef
	r.skip(12)  // hori: ascender, descender, widthMax
	r.skip(12)  // vert
	s.StartGlyph = r.u16()
	s.EndGlyph = r.u16()
	s.PPEMX = r.u8()
	s.PPEMY = r.u8()
	s.BitDepth = r.u8()
	s.Flags = r.u8()
	if err := r.err; err != nil {
		return s, 0, err
	}
	return s, arrayOffset, nil
}

// readIndexSubtables walks the IndexSubtableArray for a strike.
func readIndexSubtables(r *reader, s Strike, arrayOffset uint32, idx *BitmapIndex) error {
	if arrayOffset == 0 {
		return nil
	}
	arr := r.at(arrayOffset)
	if err := arr.err; err != nil {
		return err
	}

	type arrEntry struct {
		first, last uint16
		additional  uint32
	}
	entries := make([]arrEntry, 0, s.SubtableCount)
	for i := 0; i < int(s.SubtableCount); i++ {
		e := arrEntry{
			first:      arr.u16(),
			last:       arr.u16(),
			additional: arr.u32(),
		}
		if err := arr.err; err != nil {
			return fmt.Errorf("index subtable array entry %d: %w", i, err)
		}
		entries = append(entries, e)
	}

	for i, e := range entries {
		// An IndexSubtable sits at the array offset plus this entry's additional
		// offset, not at an absolute offset of its own.
		sub := arr.at(arrayOffset + e.additional)
		if err := sub.err; err != nil {
			return fmt.Errorf("index subtable %d: %w", i, err)
		}
		if err := readIndexSubtable(sub, s, e.first, e.last, idx); err != nil {
			return fmt.Errorf("index subtable %d: %w", i, err)
		}
	}
	return nil
}

// readIndexSubtable reads one IndexSubtable. The format is the first two bytes.
func readIndexSubtable(r *reader, s Strike, first, last uint16, idx *BitmapIndex) error {
	indexFormat := r.u16()
	imageFormat := r.u16()
	imageDataOffset := r.u32()
	if err := r.err; err != nil {
		return err
	}
	if last < first {
		return fmt.Errorf("%w: index subtable covers glyphs %d..%d", ErrMalformed, first, last)
	}
	count := int(last) - int(first) + 1

	// Formats 1 and 3 store an offset for each glyph plus a terminating offset,
	// so the length of a glyph is the difference to the next one. Format 2
	// stores one size that every glyph shares.
	switch indexFormat {
	case 1, 3:
		offsets := make([]uint32, 0, count+1)
		for i := 0; i < count+1; i++ {
			offsets = append(offsets, r.u32())
		}
		if err := r.err; err != nil {
			return fmt.Errorf("index subtable offsets: %w", err)
		}
		for i := 0; i < count; i++ {
			glyph := first + uint16(i)
			if err := addEntry(idx, glyph, imageFormat, imageDataOffset, offsets[i], offsets[i+1]); err != nil {
				return err
			}
		}

	case 2:
		// imageSize is one constant used by every glyph in the range.
		imageSize := r.u32()
		bigMetricsPresent := false
		if indexFormat == 2 {
			// The constant-size format carries a metrics block whose first byte
			// is height; the presence of the block is implied by the format.
			bigMetricsPresent = r.remaining() >= 8
		}
		if err := r.err; err != nil {
			return err
		}
		if bigMetricsPresent {
			r.skip(8) // the BigGlyphMetrics record, not interpreted
		}
		for i := 0; i < count; i++ {
			glyph := first + uint16(i)
			if err := addEntry(idx, glyph, imageFormat, imageDataOffset, uint32(i)*imageSize, uint32(i+1)*imageSize); err != nil {
				return err
			}
		}

	default:
		// A format this package does not decode. Returning the entries as empty
		// would be a lie; saying so is not.
		return fmt.Errorf("%w: CBLC index subtable format %d", ErrUnsupported, indexFormat)
	}
	return nil
}

// addEntry records one glyph's bitmap location, checking the arithmetic first.
func addEntry(idx *BitmapIndex, glyph, imageFormat uint16, base, start, end uint32) error {
	if end < start {
		return fmt.Errorf("%w: glyph %d has a shrinking offset range %d..%d", ErrMalformed, glyph, start, end)
	}
	if uint64(base)+uint64(end) > uint64(1)<<32 {
		return fmt.Errorf("%w: glyph %d offset overflows", ErrMalformed, glyph)
	}
	idx.Entries[glyph] = BitmapEntry{
		Offset:      base + start,
		Length:      end - start,
		ImageFormat: imageFormat,
	}
	return nil
}

// cbdtheaderLen is the size of the CBDT table header, which precedes the first
// bitmap. The CBLC index offsets count from the start of the CBDT table, so they
// include this header and it has to be accounted for before the data is used.
//
// Measured against NotoColorEmoji.ttf: the table begins 00 03 00 00 -- major
// version 3, minor version 0 -- and the first indexed bitmap starts four bytes
// in. Treating offset 0 as the first bitmap produces bytes that look like data
// but decode as nothing, which is how this was found.
const cbdtheaderLen = 4

// BitmapData returns the raw bitmap bytes for a glyph from the CBDT table.
//
// The bytes are returned as stored, minus any per-image header the image format
// defines. Decoding the content itself -- a PNG for formats 17 and 18, a packed
// bitmap otherwise -- is left to the caller.
func (f *Font) BitmapData(e BitmapEntry) ([]byte, error) {
	b, err := f.table("CBDT")
	if err != nil {
		return nil, err
	}
	if len(b) < cbdtheaderLen {
		return nil, fmt.Errorf("%w: CBDT table is %d bytes, shorter than its %d-byte header",
			ErrMalformed, len(b), cbdtheaderLen)
	}
	if uint64(e.Offset)+uint64(e.Length) > uint64(len(b)) {
		return nil, fmt.Errorf("%w: bitmap at %d+%d is outside a %d-byte CBDT table",
			ErrMalformed, e.Offset, e.Length, len(b))
	}
	data := b[e.Offset : e.Offset+e.Length]

	// Image formats 17 and 18 (the PNG formats) prefix the image with an
	// 8-byte BigGlyphMetrics record. Measured: an indexed bitmap whose length is
	// 872 bytes begins 80 88 00 65 88 00 00 03 5f 89 50 4e 47 -- eight bytes of
	// metrics, then a gzip header, then PNG. Searching for the signature rather
	// than computing a fixed skip also survives fonts that omit the metrics.
	if e.ImageFormat == 17 || e.ImageFormat == 18 {
		if i := indexOfPNG(data); i >= 0 {
			return data[i:], nil
		}
	}
	return data, nil
}

// pngSignature is the 8-byte PNG file signature.
var pngSignature = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

// indexOfPNG finds the PNG signature inside b, or -1.
//
// It also accepts the gzip magic, which is what Noto Color Emoji actually
// carries after the metrics: those bitmaps are gzipped PNGs. Both are returned
// so the caller gets the real image bytes without this package having to know
// which of the two a given font uses.
func indexOfPNG(b []byte) int {
	for i := 0; i+8 <= len(b); i++ {
		if b[i] == pngSignature[0] && b[i+1] == pngSignature[1] && b[i+2] == pngSignature[2] && b[i+3] == pngSignature[3] {
			if bytesEqual(b[i:i+8], pngSignature) {
				return i
			}
		}
		// gzip: 1f 8b 08
		if b[i] == 0x1f && i+3 <= len(b) && b[i+1] == 0x8b && b[i+2] == 0x08 {
			return i
		}
	}
	return -1
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
