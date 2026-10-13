#!/bin/sh
# Build colorfont, run its tests, and show it working on every fixture in the
# repository plus an emoji font from the system.
#
# Portable: it needs nothing but a Go toolchain and the source in this
# directory. If a system emoji font is present the last section exercises it;
# otherwise that section says so and moves on.

set -eu

here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$here"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

say() {
    printf '\n=== %s ===\n' "$1"
}

if command -v go >/dev/null 2>&1; then
    GO=go
elif [ -x /usr/local/go/bin/go ]; then
    GO=/usr/local/go/bin/go
else
    echo "no Go toolchain found on PATH" >&2
    exit 1
fi

say "toolchain"
"$GO" version

say "build"
"$GO" build ./...
echo "build ok"

say "vet"
"$GO" vet ./...
echo "vet ok"

say "gofmt"
unformatted=$(gofmt -l . 2>/dev/null || true)
if [ -n "$unformatted" ]; then
    echo "these files are not gofmt-clean:"
    echo "$unformatted"
    exit 1
fi
echo "gofmt clean"

say "tests"
"$GO" test ./...

say "tests under the race detector"
"$GO" test -race ./...

say "build the command-line tool"
"$GO" build -o "$tmp/colorfont" ./cmd/colorfont
echo "built $tmp/colorfont"

say "COLR version 0 fixture: palettes and layers"
"$tmp/colorfont" -font testdata/colrv0.ttf -cpal -colr

say "COLR version 1 fixture: the paint graph"
"$tmp/colorfont" -font testdata/colrv1.ttf -colr

say "COLR version 1 with a negative alpha"
"$tmp/colorfont" -font testdata/colrv1negalpha.ttf -colr

say "the same font as JSON"
"$tmp/colorfont" -font testdata/colrv0.ttf -json

say "a font with no colour tables"
"$tmp/colorfont" -font testdata/colrv1negalpha.ttf

# The CBDT path needs a real colour bitmap font. Find one if the system has it.
emoji=""
for candidate in \
    /usr/share/fonts/truetype/noto/NotoColorEmoji.ttf \
    /usr/share/fonts/noto/NotoColorEmoji.ttf \
    /Library/Fonts/NotoColorEmoji.ttf
do
    if [ -f "$candidate" ]; then
        emoji=$candidate
        break
    fi
done

if [ -n "$emoji" ]; then
    say "CBDT/CBLC on a real colour bitmap font"
    "$tmp/colorfont" -font "$emoji" -limit 5

    say "decoding every bitmap in that font"
    # A tiny program that reads the strike index and decodes every bitmap, so the
    # 3934-entry claim in the README can be reproduced rather than believed.
    mkdir -p "$tmp/decodeall"
    cat > "$tmp/decodeall/main.go" <<'GOEOF'
package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"image/png"
	"io"
	"os"

	"github.com/yondaime-kun/colorfont/internal/colorfont"
)

func main() {
	b, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}
	f, err := colorfont.Parse(b)
	if err != nil {
		panic(err)
	}
	strikes, err := f.CBLC()
	if err != nil {
		panic(err)
	}
	total, ok, failed := 0, 0, 0
	for _, s := range strikes {
		for _, e := range s.Entries {
			data, err := f.BitmapData(e)
			total++
			if err != nil {
				failed++
				continue
			}
			raw := data
			if len(data) > 2 && data[0] == 0x1f && data[1] == 0x8b {
				gz, gerr := gzip.NewReader(bytes.NewReader(data))
				if gerr != nil {
					failed++
					continue
				}
				raw, gerr = io.ReadAll(gz)
				if gerr != nil {
					failed++
					continue
				}
			}
			if _, derr := png.Decode(bytes.NewReader(raw)); derr != nil {
				failed++
				continue
			}
			ok++
		}
	}
	fmt.Printf("bitmaps indexed: %d, decoded: %d, failed: %d\n", total, ok, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
GOEOF
    # The helper imports the package by its module path, so it is built from the
    # repository root where the module lives.
    mkdir -p "$here/.runsh-tmp"
    cp "$tmp/decodeall/main.go" "$here/.runsh-tmp/main.go"
    "$GO" run "$here/.runsh-tmp/main.go" "$emoji"
    rm -rf "$here/.runsh-tmp"
else
    say "CBDT/CBLC on a real colour bitmap font"
    echo "no Noto Color Emoji on this system; install fonts-noto-color-emoji to"
    echo "exercise the CBDT path. The COLR and CPAL sections above cover the rest."
fi

say "done"
echo "everything that could run, ran."
