# RESEARCH — colorfont

What the OpenType colour tables actually look like, measured against independent
implementations, and what the prior art covers.

## Method

Nothing below is written from memory. Three references were used, and where they
disagreed the bytes won:

- **`golang.org/x/image/font/sfnt`** at `v0.47.0`, downloaded and inspected, to
  establish what the Go standard route already covers. It is the baseline the gap
  is measured against.
- **fontTools 4.66.1**, an independent Python implementation that both writes and
  reads these tables. It generated every COLR and CPAL fixture, and its
  `saveXML` output is the authority each parse was checked against.
- **`NotoColorEmoji.ttf`**, installed on this machine, which carries real CBDT and
  CBLC tables (10.7 MB and 15,852 bytes) written by Google rather than by
  anything here.

`fontTools`' own `otData.py` definitions were read directly for field types,
because several of them are unusual and guessing them produced wrong parses.

## The gap, measured

`x/image/font/sfnt` parses the tables a plain font needs: `head`, `hhea`, `hmtx`,
`maxp`, `cmap`, `glyf`, `loca`, `post`, `name`, `OS/2`, plus `GSUB`/`GPOS` for
shaping. It does not touch the colour or variation tables. The strings `COLR`,
`CPAL`, `sbix`, `CBDT`, `CBLC`, `fvar`, `gvar`, `HVAR`, `MVAR`, `avar`, and
`STAT` appear in **zero files** in the package at `v0.47.0`.

A GitHub search for `COLR`, `sbix`, `CBDT`, or variable-font parsing in Go
returns no implementation. Meanwhile the two halves of font handling that do
exist are healthy: `golang/freetype` (820 stars) for rasterisation and
`go-text/typesetting` (161 stars) for shaping. The colour and variation half has
nothing.

## What is installed, which decides what can be claimed

All 105 fonts on this machine were scanned for the tables in question:

```
CBDT + CBLC   present    NotoColorEmoji.ttf
COLR + CPAL   absent     (fixtures generated with fontTools)
sbix          absent
fvar/gvar     absent
SUVAR/STAT    absent
```

This scan is why the plan states the scope before any code: a table with no
fixture and no second implementation to check against cannot be claimed, only
guessed at.

## CPAL

CPAL holds the palettes. Measured from the generated fixture (`color.ttf`):

```
version(2)              = 0
numPaletteEntries(2)    = 2
numPalettes(2)          = 1
numColorRecords(2)      = 2
offsetFirstColorRecord(4)= 14
colorRecordIndices(2 each) = [0]     // one per palette
colorIndex per record       = [0, 1] // only when numPalettes > 1
colorRecords...
```

**A colour record is BGRA, not RGBA.** For a palette declared by fontTools as
`[#FF0000FF, #0000FFFF]` — red then blue — the bytes are:

```
record[0] = 00 00 ff ff   -> B=00 G=00 R=ff A=ff  -> #ff0000ff  red
record[1] = ff 00 00 ff   -> B=ff G=00 R=00 A=ff  -> #0000ffff  blue
```

Reading them as RGBA swaps red and blue. The alpha byte is *not* premultiplied
and the stored order is B, G, R, A from the first byte.

An earlier parse of the same fixture produced `numPaletteEntries=1,
numPalettes=2`, which is the two fields the wrong way round, and a colour of
`#ff0000ff` for a record that is actually blue. Both are recorded because both
looked plausible.

## COLR version 0

The header is **offset-based**, not a flat array, which was the second wrong
turn:

```
version(2)                   = 0
numBaseGlyphRecords(2)       = 1
baseGlyphRecordsOffset(4)    = 14   // LOffset, 32-bit, from the table start
layerRecordsOffset(4)        = 20   // LOffset, 32-bit
numLayerRecords(2)           = 2
```

Then, at those offsets:

```
baseGlyphRecords[n]: { glyphID(2), firstLayerIndex(2), numLayers(2) }
layerRecords[n]:     { glyphID(2), paletteIndex(2) }
```

Reading the whole header as a run of `uint16` gives
`numBaseGlyphRecords=1, numLayerRecords=0` and a base glyph of gid 20 with 1
layer, all of which is wrong. The two offsets are **4 bytes each**, which is what
breaks a `uint16` reading.

From the fixture, the parse that matches fontTools exactly:

```
version=0  numBaseGlyphRecords=1  baseOffset=14  layerOffset=20  numLayerRecords=2
  base[0]:   glyph=1 (A)  firstLayer=0  numLayers=2
  layer[0]:  glyph=2 (A.l1)  paletteIndex=0
  layer[1]:  glyph=3 (A.l2)  paletteIndex=1
```

and fontTools reports `A -> [(A.layer1, 0), (A.layer2, 1)]`.

## COLR version 1

Version 1 keeps the v0 header and appends five more offset fields:

```
version(2)                   = 1
numBaseGlyphRecords(2)       = 0
baseGlyphRecordsOffset(4)    = 0
layerRecordsOffset(4)        = 0
numLayerRecords(2)           = 0
baseGlyphListOffset(4)       = 34      // LOffset
layerListOffset(4)           = 50      // LOffset
clipListOffset(4)            = 0       // may be NULL
varIndexMapOffset(4)         = 0       // may be NULL
varStoreOffset(4)            = 0       // may be NULL
```

`BaseGlyphList`:

```
baseGlyphCount(4)
records[n]: { baseGlyph(2), paintOffset(4) }
```

`LayerList`:

```
layerCount(4)
paintOffsets[n](4 each)
paint records, at those offsets
```

### Where the offset bases are, measured

This is the part that no description states and that took the longest to pin
down. From the fixture, with the LayerList at absolute offset 794:

```
paint[0] record sits at 806, and its format byte is at 806
paint[1] record sits at 817
```

- **A `paintOffset` in `LayerList` is relative to the start of the `LayerList`**,
  not to the table: `794 + 12 = 806` and `794 + 23 = 817`, both correct.
- **A `paintOffset` inside a `BaseGlyphPaintRecord` is relative to the start of
  the `BaseGlyphList`**, not the table: the record at 778 holds offset 10, and
  `778 + 10 = 788`, which is where the PaintColrLayers actually is.
- **A `Paint` reference inside a paint record is relative to that paint record's
  own first byte.** `paint[0]` at 806 holds offset 6, and `806 + 6 = 812`, which
  is the PaintSolid. Reading it as relative to the LayerList gives 800, which is
  a layer-offset field, and the parse silently produces a format byte of 0.

Three different bases in one table, and two of the three produced a plausible
but wrong parse before being checked against fontTools' XML.

### Paint record fields

The field types come from `fontTools.ttLib.tables.otData`, read rather than
assumed:

```
Format 1  PaintColrLayers  { NumLayers: uint8, FirstLayerIndex: uint32 }
Format 2  PaintSolid       { PaletteIndex: uint16, Alpha: F2Dot14 }
Format 10 PaintGlyph       { Paint: Offset24, Glyph: GlyphID(uint16) }
Format 12 PaintTransform   { Paint: Offset24, Transform: Affine2x3 }
```

Two details matter:

- **`Paint` inside a paint record is an Offset24 (three bytes), not an Offset32.**
  Reading four bytes consumes a byte of the glyph id and turns gid 2 into 514.
- **`Alpha` is F2Dot14, a signed 16-bit value divided by 16384.** `0x4000` is
  1.0. Negative alphas exist in the format and must be sign-extended, which is
  why the field is read as signed.

The format numbers themselves were guessed wrong once: `12` was assumed to be
PaintGlyph when it is PaintTransform, and the correct PaintGlyph is `10`. The
numbers above are read from the converter table, not recalled.

### The parse that matches

For the v1 fixture, after all three offset bases are right:

```
BaseGlyphList: count=1
  record[0]: baseGlyph=1 (A)  paintOffset=10 -> 788
              PaintColrLayers NumLayers=2 FirstLayerIndex=0
LayerList: layerCount=2, offsets [12, 23]
  paint[0] @806: PaintGlyph glyph=2 (A.l1) -> @812 PaintSolid paletteIndex=0 alpha=1.0000
  paint[1] @817: PaintGlyph glyph=3 (A.l2) -> @823 PaintSolid paletteIndex=2 alpha=1.0000
```

fontTools' XML for the same file:

```
A -> PaintColrLayers NumLayers=2 FirstLayerIndex=0
  layer 0: PaintGlyph Glyph=A.l1 + PaintSolid PaletteIndex=0 Alpha=1.0
  layer 1: PaintGlyph Glyph=A.l2 + PaintSolid PaletteIndex=2 Alpha=1.0
```

Every field agrees.

## CBDT and CBLC

These are the bitmap colour tables, and the installed font carries them:

```
NotoColorEmoji.ttf, 10,788,068 bytes, 13 tables
  CBDT  10,697,936 bytes    the bitmap data
  CBLC      15,852 bytes    the location index
```

The CBLC structure is a three-level index, which is why a naive reader that
treats it as one table gets nothing useful out of it:

```
header: majorVersion(2) minorVersion(2) numSizes(4)
  for each strike (BitmapSize table):
    indexSubtableArrayOffset(4)  indexTablesSize(4)  numberOfIndexSubtables(4)
    colorRef(4)  hori(12) vert(12)  startGlyphIndex(2) endGlyphIndex(2) ppemX(1) ppemY(1)
    bitDepth(1) flags(1)
    then, at indexSubtableArrayOffset:
      indexSubtableArray[n]: { firstGlyphIndex(2) lastGlyphIndex(2) additionalOffset(4) }
      then each IndexSubtable, whose format depends on its first four bytes:
        format 1: { indexFormat(2) imageFormat(2) imageDataOffset(4) offsets[] }
        format 2: constant size
        format 3: explicit offsets
```

Each strike carries its own `ppemX`, `ppemY`, `bitDepth`, and `colorRef`, and a
font may have several strikes at different sizes.

## Prior art

| project | stars | language | covers |
|---|---|---|---|
| `golang/freetype` | 820 | Go | rasterising, no colour tables |
| `go-text/typesetting` | 161 | Go | shaping, no colour tables |
| `golang.org/x/image/font/sfnt` | stdlib-adjacent | Go | plain tables only |
| `fontTools` | — | Python | everything; the reference used here |

For COLR, CPAL, CBDT, CBLC, sbix, and the variation tables specifically, a
GitHub search in Go returns no implementation. This package is the first, which
is also why it leans on fontTools for verification: there is no Go peer to
compare against.

## Out of scope, and why

**sbix** (Apple's bitmap colour format) has no font on this machine and was not
generated, because generating a fixture whose correctness there is no second
implementation to check is the trap the keystore project documented.

**The variation tables** (`fvar`, `gvar`, `HVAR`, `MVAR`, `avar`, `STAT`) need a
real variable font. None is installed. fontTools can build one, but a
self-authored variable font whose deltas cannot be checked against another reader
would prove only that this package agrees with itself.

Both are reported as unsupported rather than approximated.

## Sources

- `golang.org/x/image@v0.47.0`, `font/sfnt/`, inspected for the tables it covers.
- `fontTools 4.66.1`: `colorLib/builder.py`, `colorLib/table_builder.py`,
  `ttLib/tables/otData.py`, `ttLib/tables/C_O_L_R_.py`, `ttLib/tables/C_P_A_L_.py`.
- `NotoColorEmoji.ttf` from the system font directory, for CBDT/CBLC.
- The OpenType specification for CBDT and CBLC strike layout, checked against the
  bytes of the installed font rather than taken on trust.
