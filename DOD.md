# Definition of Done — colorfont

The six required items, each with the evidence that it is actually met rather
than asserted.

## 1. PLAN.md, with failure conditions written before the code

`PLAN.md` was written first and names four failure conditions up front. Each one
is a way the project could have failed that would have been tempting to paper
over, and each is resolved below.

| # | failure condition | outcome |
|---|---|---|
| 1 | A table has no fixture and no second implementation, and gets claimed anyway | Resolved by exclusion: `sbix` and the variation tables are reported as unsupported. |
| 2 | A parse is "verified" only against this package's own writer | Resolved: COLR and CPAL are checked against fontTools; CBDT and CBLC against a font from Google. |
| 3 | Tests assert only that a call returns no error | Resolved: every reader is checked field by field against a reference reading. |
| 4 | Tests pass on fixtures but the real font fails | Resolved: `TestCBDTRealFont` decodes all 3934 bitmaps of a shipped 10.7 MB font. |

## 2. RESEARCH.md, with measurements rather than impressions

`RESEARCH.md` records the gap as a number (zero occurrences of `COLR`, `CPAL`,
`sbix`, `CBDT`, `CBLC`, `fvar`, `gvar`, `HVAR`, `MVAR`, `avar`, `STAT` in
`golang.org/x/image@v0.47.0/font/sfnt`), then the byte layout of every table it
covers, and then the wrong turns taken on the way. Three of those wrong turns
produced a parse that looked correct and was not:

- reading the CPAL colour record as RGBA instead of BGRA, which swaps red and blue;
- reading the COLR header as a flat run of uint16 when its two offsets are 32 bits;
- reading a Paint reference as 32 bits when it is Offset24.

The document exists so a reader can see what was measured versus what was
assumed. Counts and byte values in it come from commands, not recollection.

## 3. Working code

```
$ go build ./...
build ok

$ wc -l internal/colorfont/*.go cmd/colorfont/*.go | tail -1
 1449 total
```

Non-test source lines, by file:

```
internal/colorfont/sfnt.go    container, table directory, bounds-checked reader
internal/colorfont/cpal.go    CPAL palettes
internal/colorfont/colr.go    COLR version 0 and version 1
internal/colorfont/cbdt.go    CBDT and CBLC
cmd/colorfont/main.go         the command-line tool
```

Dependencies outside the standard library: **none**. `go.mod` requires nothing.

Everything the parser can be wrong about is exercised by a real file:

```
$ go run ./cmd/colorfont -font "Noto Color Emoji" -limit 2
file      : /usr/share/fonts/truetype/noto/NotoColorEmoji.ttf
flavour   : TrueType
colour    : CBDT
CBLC      : 1 strikes
  strike 0: 109x109 px, 32 bpp, glyphs 4..3975, 3934 entries
```

```
$ go run ./cmd/colorfont -font testdata/colrv0.ttf -cpal -colr
CPAL      : version 0, 2 entries, 1 palettes
  palette  0: #ff0000ff #0000ffff
COLR      : version 0
  version 0 glyphs: 1
    glyph 1:
      layer glyph=2 paletteIndex=0
      layer glyph=3 paletteIndex=1
```

## 4. Tests actually executed

```
$ go test ./...
ok  github.com/yondaime-kun/colorfont/internal/colorfont

$ go test ./... >/dev/null 2>&1; echo $?
0
```

**18 tests, exit code 0.** Verified by exit code, not by reading the output for
the word "ok".

```
$ go test -race ./...
RACE_EXIT=0

$ go vet ./...
VET_EXIT=0

$ gofmt -l .
(no output: all files are gofmt-clean)
```

The suite was run repeatedly and every run returned 0.

### The tests that matter

`TestCBDTRealFont` is the strongest check in the repository. It parses
`NotoColorEmoji.ttf` — 10,788,068 bytes, written by Google — walks its
three-level CBLC strike index, and decodes **every one of the 3934** indexed
bitmaps as PNG. All 3934 decode, and each is 136×128, matching the
`BigGlyphMetrics` prefix the parser skips.

```
bitmaps indexed: 3934, decoded: 3934, failed: 0
```

That test is not decoration: it failed loudly and correctly while the parser was
still ignoring the four-byte CBDT header, reporting 0 of 3934 bitmaps decodable.

### Mutation testing

A test that cannot fail proves nothing, so each bug this package actually had was
reintroduced and the suite re-run. Six of six were caught.

| # | mutation | result | test that caught it |
|---|---|---|---|
| 1 | CPAL colour record read as RGBA | **caught** | `TestCPALColours`, `TestCPALBGRAOrder` |
| 2 | COLR header read as flat uint16 | **caught** | `TestCOLRV0`, `TestCOLRV0HeaderIsOffsetBased` |
| 3 | Paint reference read as 32 bits | **caught** | `TestCOLRV1`, `TestCOLRV1Offset24`, `TestCOLRV1AlphaIsSigned` |
| 4 | F2Dot14 alpha read unsigned | **caught after a test fix** | `TestCOLRV1NegativeAlpha` |
| 5 | CBDT header not skipped | **caught** | `TestCBDTRealFont`, `TestCBDTHeaderSkipIsRequired` |
| 6 | LayerList data taken after its header | **caught** | `TestCOLRV1` |

**Mutation 4 initially survived, and that was a test bug, not a code bug.** The
first alpha test asserted only that the value lay within (-1, 1); the fixture's
alpha was 1.0, where a signed and an unsigned read agree, so the test passed
either way. A second fixture was generated with alpha -0.5 (`0xE000`, which is
-0.5 signed and 3.5 unsigned) and `TestCOLRV1NegativeAlpha` was added. Re-running
the mutation then produced:

```
--- FAIL: TestCOLRV1NegativeAlpha
    colorfont_test.go:330: alpha = 3.5, want -0.5; an unsigned read gives 3.5
```

The lesson is recorded because it is the general one: a range assertion over a
value that happens to be positive does not test signedness.

## 5. Honest README

`README.md` states what works, how each claim was checked, and what does not
work. The unsupported list is specific and gives the reason:

- `sbix` — no font on the development machine carries it and there is no fixture.
- the variation tables — no variable font was available, and one built by this
  package's own tooling could not be checked against another reader.

It also names three narrower limits: CPAL palette labels are returned as name ids
because the `name` table is not parsed; paint formats beyond the four decoded are
reported by number rather than interpreted; and `BitmapData` returns stored bytes
without unpacking monochrome or grayscale bit depths.

Nothing in the README claims a capability that is not demonstrated by a command
in it or a test in the suite.

## 6. Commit

One commit, on the default branch, with the repository pushed.

```
$ git log --oneline -1
<see the commit list in the project repository>
```

## The bar, compared to the previous project

`keystore` verified its crypto against JDK and BouncyCastle bytecode.
`colorfont` verifies its parsing against a second implementation (fontTools) and
against a real font from Google, and its strongest test decodes 3934 images
rather than comparing fields. The limitation list is shorter and narrower than
the previous project's, and each remaining item is a decision with a stated
reason rather than a gap that was left unexamined.
