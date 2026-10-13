// Package colorfont reads the colour tables of OpenType and TrueType fonts.
//
// It covers the SFNT container, CPAL palettes, COLR colour layers in both
// versions, and the CBDT/CBLC bitmap colour tables. The Go standard library's
// sfnt reader covers none of these: the colour and variation half of OpenType
// has no implementation in Go, which is why this package exists.
//
// Every structural fact in this file was measured against files written by other
// implementations -- fontTools for COLR and CPAL, Google's NotoColorEmoji for
// CBDT and CBLC -- rather than taken from a description. RESEARCH.md records the
// measurements and the wrong turns.
package colorfont

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Errors returned by this package.
var (
	// ErrNotFont is returned when the bytes are not an SFNT container.
	ErrNotFont = errors.New("colorfont: not an SFNT font")
	// ErrNoTable is returned when a font does not carry a requested table.
	ErrNoTable = errors.New("colorfont: font has no such table")
	// ErrMalformed is returned for a table whose contents contradict its own
	// declared sizes.
	ErrMalformed = errors.New("colorfont: malformed table")
	// ErrUnsupported is returned for a table this package deliberately does not
	// implement, with the reason in the message.
	ErrUnsupported = errors.New("colorfont: unsupported")
)

// sfnt magic numbers. The first four bytes of a font identify the flavour.
const (
	magicTrueType = 0x00010000 // TrueType outlines
	magicOTTO     = 0x4F54544F // "OTTO", CFF outlines
	magicTrue     = 0x74727565 // "true", older Apple TrueType
	magicTTCF     = 0x74746366 // "ttcf", a font collection
)

// Font is a parsed SFNT container.
type Font struct {
	// Flavour is the first four bytes, as a string: "\x00\x01\x00\x00",
	// "OTTO", "true", or "ttcf".
	Flavour string

	// IsCollection reports whether this is a TrueType collection, which holds
	// several fonts in one file.
	IsCollection bool

	// NumFonts is the number of fonts in a collection, or 1 for a plain font.
	NumFonts int

	// tables maps a four-character tag to its location in the file.
	tables map[string]tableRecord

	data []byte
}

type tableRecord struct {
	offset uint32
	length uint32
}

// Parse reads the container and its table directory. It does not parse any
// individual table; those are done on demand.
//
// The reader never allocates based on a length taken from the file without first
// checking that length against the bytes actually present, because a font is
// untrusted input.
func Parse(data []byte) (*Font, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("%w: %d bytes is too short for a header", ErrNotFont, len(data))
	}
	magic := binary.BigEndian.Uint32(data[0:4])
	f := &Font{data: data, NumFonts: 1}

	switch magic {
	case magicTrueType, magicOTTO, magicTrue:
		f.Flavour = string(data[0:4])
	case magicTTCF:
		f.Flavour = "ttcf"
		f.IsCollection = true
	default:
		return nil, fmt.Errorf("%w: magic 0x%08X", ErrNotFont, magic)
	}

	if f.IsCollection {
		// A collection starts with a header listing the offsets of its fonts.
		// This reader reports the collection and parses the first font, which is
		// what a caller inspecting a .ttc almost always wants; the offsets of
		// the others are validated so a truncated collection is still caught.
		if len(data) < 12 {
			return nil, fmt.Errorf("%w: collection header is short", ErrNotFont)
		}
		n := binary.BigEndian.Uint32(data[8:12])
		if n == 0 {
			return nil, fmt.Errorf("%w: collection declares no fonts", ErrMalformed)
		}
		if int(n) > (len(data)-12)/4 {
			return nil, fmt.Errorf("%w: collection claims %d fonts but the file holds %d offsets",
				ErrMalformed, n, (len(data)-12)/4)
		}
		f.NumFonts = int(n)
		first := binary.BigEndian.Uint32(data[12:16])
		if int(first) >= len(data) {
			return nil, fmt.Errorf("%w: first font offset %d is past the end", ErrMalformed, first)
		}
		return f.parseDirectory(int(first))
	}
	return f.parseDirectory(0)
}

// parseDirectory reads the table directory that starts at base.
func (f *Font) parseDirectory(base int) (*Font, error) {
	b := f.data
	if base+12 > len(b) {
		return nil, fmt.Errorf("%w: table directory at %d is past the end", ErrMalformed, base)
	}
	numTables := int(binary.BigEndian.Uint16(b[base+4 : base+6]))
	// Each record is 16 bytes. A count larger than the file can hold is hostile.
	if numTables > (len(b)-base-12)/16 {
		return nil, fmt.Errorf("%w: %d table records do not fit in %d bytes",
			ErrMalformed, numTables, len(b)-base-12)
	}

	f.tables = make(map[string]tableRecord, numTables)
	for i := 0; i < numTables; i++ {
		o := base + 12 + i*16
		tag := string(b[o : o+4])
		offset := binary.BigEndian.Uint32(b[o+8 : o+12])
		length := binary.BigEndian.Uint32(b[o+12 : o+16])
		// The record must describe a region inside the file.
		if uint64(offset)+uint64(length) > uint64(len(b)) {
			return nil, fmt.Errorf("%w: table %q claims bytes %d..%d of a %d-byte file",
				ErrMalformed, tag, offset, uint64(offset)+uint64(length), len(b))
		}
		f.tables[tag] = tableRecord{offset: offset, length: length}
	}
	return f, nil
}

// TableTags returns every table tag in the font, in the order they appear.
func (f *Font) TableTags() []string {
	out := make([]string, 0, len(f.tables))
	for _, r := range f.order() {
		out = append(out, r)
	}
	return out
}

// order returns the tags sorted, so output is stable between runs. A map
// iteration order would make every listing differ.
func (f *Font) order() []string {
	tags := make([]string, 0, len(f.tables))
	for t := range f.tables {
		tags = append(tags, t)
	}
	// Insertion sort: the tag count is small and this keeps the package free of
	// a sort import solely for a dozen strings.
	for i := 1; i < len(tags); i++ {
		for j := i; j > 0 && tags[j] < tags[j-1]; j-- {
			tags[j], tags[j-1] = tags[j-1], tags[j]
		}
	}
	return tags
}

// HasTable reports whether the font carries a table.
func (f *Font) HasTable(tag string) bool {
	_, ok := f.tables[tag]
	return ok
}

// table returns the raw bytes of a table.
func (f *Font) table(tag string) ([]byte, error) {
	r, ok := f.tables[tag]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoTable, tag)
	}
	return f.data[r.offset : r.offset+r.length], nil
}

// ColourMechanism names how a font carries colour, if at all.
type ColourMechanism string

const (
	// ColourNone means the font has no colour tables this package recognises.
	ColourNone ColourMechanism = ""
	// ColourCOLR means vector colour through COLR, with a CPAL palette.
	ColourCOLR ColourMechanism = "COLR"
	// ColourCBDT means embedded bitmap colour through CBDT and CBLC.
	ColourCBDT ColourMechanism = "CBDT"
	// ColourSBIT means Apple's sbix bitmap format, which is not implemented.
	ColourSBIT ColourMechanism = "sbix"
)

// Colour reports how, and whether, the font carries colour.
//
// A font may carry more than one mechanism; the first found in this order is
// returned, which matches what a renderer prefers: vector COLR beats embedded
// bitmaps, which beat the unimplemented sbix.
func (f *Font) Colour() ColourMechanism {
	switch {
	case f.HasTable("COLR") && f.HasTable("CPAL"):
		return ColourCOLR
	case f.HasTable("CBDT") && f.HasTable("CBLC"):
		return ColourCBDT
	case f.HasTable("sbix"):
		return ColourSBIT
	}
	return ColourNone
}

// reader is a bounds-checked cursor over a table's bytes.
//
// Every read checks the remaining length first. A font is untrusted input and a
// truncated table must produce an error, not a panic or a silent misread.
type reader struct {
	b   []byte
	off int
	err error
}

func newReader(b []byte) *reader { return &reader{b: b} }

func (r *reader) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("%w: "+format, append([]any{ErrMalformed}, args...)...)
	}
}

func (r *reader) remaining() int { return len(r.b) - r.off }

func (r *reader) u8() uint8 {
	if r.err != nil {
		return 0
	}
	if r.off+1 > len(r.b) {
		r.fail("needs 1 byte at offset %d of %d", r.off, len(r.b))
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}

func (r *reader) u16() uint16 {
	if r.err != nil {
		return 0
	}
	if r.off+2 > len(r.b) {
		r.fail("needs 2 bytes at offset %d of %d", r.off, len(r.b))
		return 0
	}
	v := binary.BigEndian.Uint16(r.b[r.off : r.off+2])
	r.off += 2
	return v
}

func (r *reader) i16() int16 {
	return int16(r.u16())
}

func (r *reader) u24() uint32 {
	if r.err != nil {
		return 0
	}
	if r.off+3 > len(r.b) {
		r.fail("needs 3 bytes at offset %d of %d", r.off, len(r.b))
		return 0
	}
	v := uint32(r.b[r.off])<<16 | uint32(r.b[r.off+1])<<8 | uint32(r.b[r.off+2])
	r.off += 3
	return v
}

func (r *reader) u32() uint32 {
	if r.err != nil {
		return 0
	}
	if r.off+4 > len(r.b) {
		r.fail("needs 4 bytes at offset %d of %d", r.off, len(r.b))
		return 0
	}
	v := binary.BigEndian.Uint32(r.b[r.off : r.off+4])
	r.off += 4
	return v
}

// skip advances past n bytes, checking them first.
func (r *reader) skip(n int) {
	if r.err != nil {
		return
	}
	if n < 0 || r.off+n > len(r.b) {
		r.fail("cannot skip %d bytes at offset %d of %d", n, r.off, len(r.b))
		return
	}
	r.off += n
}

// at produces a reader positioned at an absolute offset within the same table.
// It is how an offset field is followed: the offset becomes a fresh cursor.
func (r *reader) at(off uint32) *reader {
	if r.err != nil {
		return r
	}
	if int(off) > len(r.b) {
		r.fail("offset %d is past the end of a %d-byte table", off, len(r.b))
		return r
	}
	return &reader{b: r.b, off: int(off)}
}

// DebugTableSize reports the offset and length of a table, for tests and tools.
func DebugTableSize(data []byte, tag string) (length, offset uint32) {
	f, err := Parse(data)
	if err != nil {
		return 0, 0
	}
	r, ok := f.tables[tag]
	if !ok {
		return 0, 0
	}
	return r.length, r.offset
}
