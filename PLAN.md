# PLAN — colorfont

A dependency-free reader for the colour and variation tables inside OpenType and
TrueType fonts, in Go.

## Why this target

`golang.org/x/image/font/sfnt` is the standard way to read fonts in Go. It is a
good reader for the tables it covers, and it does not cover any of the tables
that make a font carry colour or variation. Measured directly against
`x/image@v0.47.0`: the strings `COLR`, `CPAL`, `sbix`, `CBDT`, `CBLC`, `fvar`,
`gvar`, `HVAR`, `MVAR`, `avar`, and `STAT` appear in **zero** files in
`font/sfnt`.

A GitHub search for `COLR`, `sbix`, `CBDT`, or variable-font parsing in Go returns
nothing relevant. The colour and variation half of OpenType is unimplemented in
Go, while the rasteriser (`golang/freetype`, 820 stars) and the shaper
(`go-text/typesetting`, 161 stars) both exist. So a Go program that wants to know
what colour a glyph is, or what a variable font's axis does, has nowhere to look.

That is a real gap rather than a preference, and it sits in the same class as the
rest of this portfolio: a binary format with published structure, real
production use, and no Go implementation.

## What is actually achievable, and what is not

The fonts available on this machine decide the scope, because a table that cannot
be tested against a real font must not be claimed. A scan of all 105 installed
fonts found:

```
CBDT + CBLC   present   NotoColorEmoji.ttf (CBDT 10.7 MB, CBLC 15,852 bytes)
COLR + CPAL   absent    (generated for testing with fontTools)
sbix          absent    (not generated - see below)
fvar/gvar     absent    (not generated - see below)
```

So:

- **In scope: COLR v0 and v1, CPAL, CBDT, CBLC, and the SFNT container.**
  COLR/CPAL has no installed font, so fixtures are generated with fontTools,
  which is an independent implementation and writes the same tables.
- **Out of scope: sbix, and the variation tables (fvar, gvar, HVAR, MVAR, avar,
  STAT).** Stating an axis or a variable-glyph delta needs a real variable font
  and none is installed; fontTools can build one, but building a fixture whose
  correctness I cannot check against a second implementation is exactly the
  trap the keystore project documented. These are reported as unsupported, not
  approximated.

## Failure conditions, written before the code

1. **If the reader produces a colour for a glyph that disagrees with an
   independent implementation, the reader is wrong.** fontTools reads the same
   files and reports the palette and the layers; any disagreement is a bug in
   this package.

2. **If a fixture cannot be produced by an independent tool, the table is not
   claimed as supported.** This is what removes sbix and the variation tables
   from scope above.

3. **If the reader cannot agree with itself across the two container versions
   (COLR v0 and v1) on a font that carries both, the version handling is wrong.**

4. **If a truncated or hostile table causes a panic, the reader is unsafe.** Font
   files are untrusted input; every length must be bounded before use.

5. **If the only evidence for a table is that this package can read a file this
   package wrote, nothing has been shown.**

## Deliverable

A library and a CLI that, given a font file, reports:

- the SFNT container and its table directory,
- whether the font is colour and by which mechanism,
- for COLR fonts: the layers of a glyph and the palette entry each layer uses,
- for CPAL: the palettes and their colours, resolved to RGBA,
- for bitmap colour fonts: the strike sizes in CBLC and whether a glyph has
  bitmap data in CBDT,
- and, explicitly, that the variation tables are not supported.

## Non-goals

- No rasterisation. This reads tables, it does not draw glyphs.
- No font editing or writing.
- No glyph outline interpretation beyond what is needed to identify a glyph.
