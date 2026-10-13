// Command colorfont prints what a font carries: its flavour, its tables, its
// colour mechanism, and on request its palettes and colour layers.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/yondaime-kun/colorfont/internal/colorfont"
)

func main() {
	var (
		fontPath = flag.String("font", "", "path to a font file, or a font family name for fontconfig to resolve")
		showCOLR = flag.Bool("colr", false, "print the colour layers")
		showCPAL = flag.Bool("cpal", false, "print the palettes")
		asJSON   = flag.Bool("json", false, "print as JSON")
		maxDump  = flag.Int("limit", 40, "cap the number of glyphs or entries printed")
	)
	flag.Parse()

	if *fontPath == "" && flag.NArg() > 0 {
		*fontPath = flag.Arg(0)
	}
	if *fontPath == "" {
		fmt.Fprintln(os.Stderr, "colorfont: give a font with -font, or a family name as an argument")
		flag.Usage()
		os.Exit(2)
	}

	path, err := resolveFont(*fontPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "colorfont: %v\n", err)
		os.Exit(1)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "colorfont: %v\n", err)
		os.Exit(1)
	}
	font, err := colorfont.Parse(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "colorfont: %v\n", err)
		os.Exit(1)
	}

	if *asJSON {
		if err := printJSON(path, font, *maxDump); err != nil {
			fmt.Fprintf(os.Stderr, "colorfont: %v\n", err)
			os.Exit(1)
		}
		return
	}

	printSummary(path, font)
	if *showCPAL {
		printCPAL(font)
	}
	if *showCOLR {
		printCOLR(font, *maxDump)
	}
	if font.Colour() == colorfont.ColourCBDT {
		printStrikes(font, *maxDump)
	}
}

// resolveFont returns a filesystem path. A path that exists is used as is;
// anything else is handed to fc-match, which maps a family name to a file.
func resolveFont(s string) (string, error) {
	if _, err := os.Stat(s); err == nil {
		return s, nil
	}
	if _, err := os.Stat(s + ".ttf"); err == nil {
		return s + ".ttf", nil
	}

	// fontconfig is not required; its absence just means family names do not work.
	if _, err := exec.LookPath("fc-match"); err != nil {
		return "", fmt.Errorf("no such file %q, and fontconfig is not installed to resolve a family name", s)
	}
	out, err := exec.Command("fc-match", "-f", "%{file}", s).Output()
	if err != nil {
		return "", fmt.Errorf("fc-match %q: %w", s, err)
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return "", fmt.Errorf("fontconfig resolved %q to nothing", s)
	}
	return p, nil
}

func printSummary(path string, f *colorfont.Font) {
	fmt.Printf("file      : %s\n", path)
	fmt.Printf("flavour   : %s\n", flavourName(f))
	if f.IsCollection {
		fmt.Printf("collection: %d fonts\n", f.NumFonts)
	}
	fmt.Printf("tables    : %d\n", len(f.TableTags()))
	fmt.Printf("  %s\n", strings.Join(f.TableTags(), " "))

	c := f.Colour()
	if c == "" {
		fmt.Println("colour    : none")
	} else {
		fmt.Printf("colour    : %s\n", c)
	}
}

func flavourName(f *colorfont.Font) string {
	switch f.Flavour {
	case "\x00\x01\x00\x00":
		return "TrueType"
	case "OTTO":
		return "OpenType (CFF)"
	case "true":
		return "TrueType (Apple)"
	case "ttcf":
		return "TrueType collection"
	}
	return fmt.Sprintf("%q", f.Flavour)
}

func printCPAL(f *colorfont.Font) {
	cp, err := f.CPAL()
	if err != nil {
		fmt.Printf("CPAL      : %v\n", err)
		return
	}
	fmt.Printf("CPAL      : version %d, %d entries, %d palettes\n",
		cp.Version, cp.NumPaletteEntries, len(cp.Palettes))
	for i, p := range cp.Palettes {
		hexes := make([]string, 0, len(p.Colours))
		for _, c := range p.Colours {
			hexes = append(hexes, c.Hex())
		}
		fmt.Printf("  palette %2d: %s\n", i, strings.Join(hexes, " "))
	}
}

func printCOLR(f *colorfont.Font, limit int) {
	c, err := f.COLR()
	if err != nil {
		fmt.Printf("COLR      : %v\n", err)
		return
	}
	fmt.Printf("COLR      : version %d\n", c.Version)

	if len(c.V0Layers) > 0 {
		fmt.Printf("  version 0 glyphs: %d\n", len(c.V0Layers))
		n := 0
		for g, ls := range c.V0Layers {
			if n >= limit {
				fmt.Printf("    ... %d more\n", len(c.V0Layers)-n)
				break
			}
			fmt.Printf("    glyph %d:\n", g)
			for _, l := range ls {
				fmt.Printf("      layer glyph=%d paletteIndex=%d\n", l.GlyphID, l.PaletteIndex)
			}
			n++
		}
	}
	if len(c.V1) > 0 {
		fmt.Printf("  version 1 glyphs: %d\n", len(c.V1))
		n := 0
		for g, p := range c.V1 {
			if n >= limit {
				fmt.Printf("    ... %d more\n", len(c.V1)-n)
				break
			}
			fmt.Printf("    glyph %d:\n", g)
			printPaint(p, "      ")
			n++
		}
	}
}

func printPaint(p *colorfont.Paint, indent string) {
	if p == nil {
		return
	}
	switch p.Format {
	case 1:
		fmt.Printf("%sPaintColrLayers layers=%d\n", indent, len(p.Layers))
	case 2:
		fmt.Printf("%sPaintSolid paletteIndex=%d alpha=%.4f\n", indent, p.PaletteIndex, p.Alpha)
	case 3:
		fmt.Printf("%sPaintVarSolid paletteIndex=%d alpha=%.4f\n", indent, p.PaletteIndex, p.Alpha)
	case 10:
		fmt.Printf("%sPaintGlyph glyph=%d\n", indent, p.GlyphID)
	default:
		fmt.Printf("%spaint format %d (not decoded)\n", indent, p.Format)
	}
	for _, c := range p.Layers {
		printPaint(c, indent+"  ")
	}
	if p.Paint != nil {
		printPaint(p.Paint, indent+"  ")
	}
}

func printStrikes(f *colorfont.Font, limit int) {
	strikes, err := f.CBLC()
	if err != nil {
		fmt.Printf("CBLC      : %v\n", err)
		return
	}
	fmt.Printf("CBLC      : %d strikes\n", len(strikes))
	for i, s := range strikes {
		fmt.Printf("  strike %d: %dx%d px, %d bpp, glyphs %d..%d, %d entries\n",
			i, s.Strike.PPEMX, s.Strike.PPEMY, s.Strike.BitDepth,
			s.Strike.StartGlyph, s.Strike.EndGlyph, len(s.Entries))
		n := 0
		for g, e := range s.Entries {
			if n >= limit {
				fmt.Printf("    ... %d more\n", len(s.Entries)-n)
				break
			}
			fmt.Printf("    glyph %-5d offset=%-9d length=%-6d imageFormat=%d\n",
				g, e.Offset, e.Length, e.ImageFormat)
			n++
		}
	}
}

// jsonFont is the shape printed by -json. It is defined here rather than reusing
// the package's types so the output stays stable even if those change.
type jsonFont struct {
	File       string            `json:"file"`
	Flavour    string            `json:"flavour"`
	Fonts      int               `json:"fonts"`
	Tables     []string          `json:"tables"`
	Colour     string            `json:"colour"`
	CPAL       *jsonCPAL         `json:"cpal,omitempty"`
	COLR       *jsonCOLR         `json:"colr,omitempty"`
	Strikes    []jsonStrike      `json:"strikes,omitempty"`
	Unreadable map[string]string `json:"unreadable,omitempty"`
}

type jsonCPAL struct {
	Version  uint16     `json:"version"`
	Entries  uint16     `json:"entries"`
	Palettes [][]string `json:"palettes"`
}

type jsonCOLR struct {
	Version uint16   `json:"version"`
	V0      []jsonV0 `json:"v0,omitempty"`
	V1      []jsonV1 `json:"v1,omitempty"`
}

type jsonV0 struct {
	Glyph  uint16      `json:"glyph"`
	Layers []jsonLayer `json:"layers"`
}

// jsonLayer is one version 0 layer. The glyph and the palette index are separate
// fields: packing them into one integer loses the meaning of both.
type jsonLayer struct {
	Glyph        uint16 `json:"glyph"`
	PaletteIndex uint16 `json:"paletteIndex"`
}

type jsonV1 struct {
	Glyph uint16     `json:"glyph"`
	Paint *jsonPaint `json:"paint"`
}

type jsonPaint struct {
	Format       uint8       `json:"format"`
	Glyph        *uint16     `json:"glyph,omitempty"`
	PaletteIndex *uint16     `json:"paletteIndex,omitempty"`
	Alpha        *float64    `json:"alpha,omitempty"`
	Layers       []jsonPaint `json:"layers,omitempty"`
	Paint        *jsonPaint  `json:"paint,omitempty"`
}

type jsonStrike struct {
	PPEMX    uint8 `json:"ppemX"`
	PPEMY    uint8 `json:"ppemY"`
	BitDepth uint8 `json:"bitDepth"`
	Entries  int   `json:"entries"`
}

func printJSON(path string, f *colorfont.Font, limit int) error {
	out := jsonFont{
		File:    path,
		Flavour: flavourName(f),
		Fonts:   f.NumFonts,
		Tables:  f.TableTags(),
		Colour:  string(f.Colour()),
	}

	if cp, err := f.CPAL(); err == nil {
		j := &jsonCPAL{Version: cp.Version, Entries: cp.NumPaletteEntries}
		for _, p := range cp.Palettes {
			row := make([]string, 0, len(p.Colours))
			for _, c := range p.Colours {
				row = append(row, c.Hex())
			}
			j.Palettes = append(j.Palettes, row)
		}
		out.CPAL = j
	} else if f.HasTable("CPAL") {
		out.setUnreadable("CPAL", err.Error())
	}

	if c, err := f.COLR(); err == nil {
		j := &jsonCOLR{Version: c.Version}
		n := 0
		for g, ls := range c.V0Layers {
			if n >= limit {
				break
			}
			row := jsonV0{Glyph: g}
			for _, l := range ls {
				row.Layers = append(row.Layers, jsonLayer{
					Glyph:        l.GlyphID,
					PaletteIndex: l.PaletteIndex,
				})
			}
			j.V0 = append(j.V0, row)
			n++
		}
		n = 0
		for g, p := range c.V1 {
			if n >= limit {
				break
			}
			j.V1 = append(j.V1, jsonV1{Glyph: g, Paint: toJSONPaint(p)})
			n++
		}
		out.COLR = j
	} else if f.HasTable("COLR") {
		out.setUnreadable("COLR", err.Error())
	}

	if strikes, err := f.CBLC(); err == nil {
		for _, s := range strikes {
			out.Strikes = append(out.Strikes, jsonStrike{
				PPEMX: s.Strike.PPEMX, PPEMY: s.Strike.PPEMY,
				BitDepth: s.Strike.BitDepth, Entries: len(s.Entries),
			})
		}
	} else if f.HasTable("CBLC") {
		out.setUnreadable("CBLC", err.Error())
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func toJSONPaint(p *colorfont.Paint) *jsonPaint {
	if p == nil {
		return nil
	}
	j := &jsonPaint{Format: p.Format}
	switch p.Format {
	case 1:
		for _, c := range p.Layers {
			j.Layers = append(j.Layers, *toJSONPaint(c))
		}
	case 2, 3:
		idx, a := p.PaletteIndex, p.Alpha
		j.PaletteIndex, j.Alpha = &idx, &a
	case 10:
		g := p.GlyphID
		j.Glyph = &g
		j.Paint = toJSONPaint(p.Paint)
	}
	return j
}

func (j *jsonFont) setUnreadable(key, msg string) {
	if j.Unreadable == nil {
		j.Unreadable = map[string]string{}
	}
	j.Unreadable[key] = msg
}
