# colorfont

Reads the colour tables inside OpenType and TrueType fonts, in Go, with no
dependencies outside the standard library.

- **CPAL** — colour palettes, with the stored BGRA order handled.
- **COLR** — vector colour layers, version 0 and version 1, including the paint
  graph that version 1 introduced.
- **CBDT / CBLC** — embedded bitmap colour, resolved through the three-level
  strike index and decoded to real images.
- **SFNT** — the container, its table directory, and TrueType collections.

## Why

The Go standard library's `golang.org/x/image/font/sfnt` reads the tables a plain
font needs. It does not read the colour or variation tables. Searching
`golang.org/x/image@v0.47.0` for `COLR`, `CPAL`, `sbix`, `CBDT`, `CBLC`, `fvar`,
`gvar`, `HVAR`, `MVAR`, `avar`, and `STAT` finds **zero** occurrences of any of
them.

The two other halves of font handling in Go are healthy: `golang/freetype` for
rasterising and `go-text/typesetting` for shaping. The colour half had nothing,
which is why this package exists.

## What works

Every claim below was checked against files this package did not write.

```
COLR v0   reads base glyph and layer records                  vs fontTools 4.66.1 XML   match
COLR v1   reads the paint graph (ColrLayers, Glyph, Solid)    vs fontTools 4.66.1 XML   match
CPAL      reads palettes, both the single- and multi-palette
          layouts, in BGRA order                              vs fontTools 4.66.1 XML   match
CBDT/CBLC reads the strike index and decodes every bitmap     vs NotoColorEmoji.ttf
                                                                 from Google           3934/3934
```

The CBDT check is the strongest one available here. It parses a 10.7 MB font
shipped by Google, walks its three-level index, and decodes **all 3934** indexed
bitmaps as PNG. Every one decodes; every one is 136×128, matching the
`BigGlyphMetrics` prefix the parser skips.

```
$ go run ./cmd/colorfont -font "$(fc-match -f '%{file}' 'Noto Color Emoji')"
flavour   : TrueType
colour    : CBDT
strikes   : 1
  strike 0: 109x109 px, 32 bpp, glyphs 4..3975, 3934 entries
  bitmaps decode: 3934 / 3934
```

## Usage

```go
data, err := os.ReadFile("NotoColorEmoji.ttf")
if err != nil {
    return err
}
font, err := colorfont.Parse(data)
if err != nil {
    return err
}

switch font.Colour() {
case colorfont.ColourCOLR:
    colr, err := font.COLR()
    if err != nil {
        return err
    }
    pal, err := font.CPAL()
    if err != nil {
        return err
    }
    // A version 0 glyph is a list of layers, each naming a glyph and a palette
    // entry.
    for _, layer := range colr.V0Layers[glyphID] {
        colour := pal.Palettes[0].Colours[layer.PaletteIndex]
        _ = colour.Hex()

    }
    // A version 1 glyph is a tree of paints.
    if paint := colr.V1[glyphID]; paint != nil {
        _ = paint.Format
    }

case colorfont.ColourCBDT:
    strikes, err := font.CBLC()
    if err != nil {
        return err
    }
    entry, ok := strikes[0].Entries[glyphID]
    if !ok {
        return fmt.Errorf("glyph %d has no bitmap in this strike", glyphID)
    }
    raw, err := font.BitmapData(entry)
    if err != nil {
        return err
    }
    // raw is a PNG for image formats 17 and 18, and a packed bitmap otherwise.
    _ = raw
}
```

## The command-line tool

```
colorfont -font Font.ttf            # summary: flavour, tables, colour mechanism
colorfont -font Font.ttf -colr      # dump the colour layers
colorfont -font Font.ttf -cpal      # dump the palettes
colorfont -font Font.ttf -json      # the same as JSON
```

`-font` accepts a path or a font family name, which is resolved with `fc-match`
when `fontconfig` is installed. Run `colorfont -h` for the rest.

## The three offset bases in COLR version 1

The hardest part of this table, and the part no description states:

- A paint offset in the **LayerList's offset array** is relative to the start of
  the LayerList.
- A paint offset in a **BaseGlyphPaintRecord** is relative to the start of the
  BaseGlyphList.
- A `Paint` reference **inside a paint record** is relative to that record.

Three different bases in one table. Two of the three produce a parse that looks
successful and is wrong, which is why every field is checked against fontTools'
XML rather than eyeballed. `RESEARCH.md` records the wrong turns, including the
one that read `paintOffset` from the wrong base and got a format byte of zero.

## The CBDT header

`CBLC` offsets count from the start of the `CBDT` table, and the first four bytes
of that table are a header (`00 03 00 00` -- major 3, minor 0). The indexed
bitmaps begin after it. Ignoring the header makes every lookup land in the wrong
place: the parser indexes bytes that look like data and decode as nothing.

Each PNG-format bitmap is also prefixed by an eight-byte `BigGlyphMetrics`
record. `BitmapData` returns the image bytes and skips that prefix, locating the
PNG by signature so a font that omits the metrics still works.

Both facts came from measuring `NotoColorEmoji.ttf`, not from a specification.

## What is not supported

Two tables are deliberately absent. Both are reported by name rather than
approximated, because a fixture whose result cannot be checked against a second
implementation proves only self-consistency.

- **`sbix`.** No font on the development machine carries it, and it has no
  fixture to test against.
- **The variation tables** (`fvar`, `gvar`, `HVAR`, `MVAR`, `avar`, `STAT`). These
  need a real variable font. None was available, and a variable font built by
  this package's own tooling could not be verified against anything.

`Font.Colour` reports `ColourSBIT` for a font carrying `sbix`, so a caller learns
what it found without this package pretending to have read it.

Additional limits:

- Resolving CPAL's palette and entry *labels* needs the `name` table, which this
  package does not parse. `CPALLabelNameIDs` returns the ids instead of guessing
  at strings.
- Paint formats beyond `PaintColrLayers`, `PaintSolid`, `PaintGlyph` and
  `PaintVarSolid` are reported by their format number and not decoded further.
  A gradient or a transform is named, not invented.
- `BitmapData` returns the stored bytes. Decoding a monochrome or grayscale
  bitmap means unpacking the bit depth, which is left to the caller; only the PNG
  formats are `image/png` input.

## Testing

```
go test ./...
```

The suite covers the container, CPAL, COLR versions 0 and 1, and CBDT/CBLC. The
real-font tests skip when Noto Color Emoji is not installed, so the suite passes
on a machine without it; they are the tests that matter, and they run here.

Several tests exist to catch specific mistakes this package made and fixed. Each
one is named for the bug it plants:

| test | the bug it catches |
|---|---|
| `TestCPALBGRAOrder` | reading the stored record as RGBA, swapping red and blue |
| `TestCOLRV0HeaderIsOffsetBased` | reading the header as a flat run of uint16 |
| `TestCOLRV1Offset24` | reading a Paint reference as 32 bits, consuming a byte of the glyph id |
| `TestCOLRV1AlphaIsSigned` | reading the F2Dot14 alpha as unsigned |
| `TestCBDTHeaderSkipIsRequired` | ignoring the four-byte CBDT header |
| `TestCBDTBitmapMetricsMatchImage` | skipping the wrong number of bytes before the PNG |

The suite has been mutated to confirm it bites: each of the bugs above was
reintroduced and the corresponding test failed. `DOD.md` records the evidence.

## Regenerating the fixtures

The COLR and CPAL fixtures are built with fontTools, which is also the
implementation they are checked against. `testdata/GENERATED.md` has the exact
commands.

## Licence

MIT. See `LICENSE`.
