package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

func main() {
	fontPath := findFile("frontend/static/fonts/FreeSerif.ttf")
	baseDir := findBase()
	outPath := filepath.Join(baseDir, "frontend", "static", "img", "quote-left.svg")

	fontBytes, err := os.ReadFile(fontPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot read font file %s: %v\n", fontPath, err)
		os.Exit(1)
	}

	font, err := sfnt.Parse(fontBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot parse font: %v\n", err)
		os.Exit(1)
	}

	// U+275D = ❝
	const targetRune = '\u275D'

	buf := new(sfnt.Buffer)
	glyphIndex, err := font.GlyphIndex(buf, targetRune)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot get glyph index for U+275D: %v\n", err)
		os.Exit(1)
	}
	if glyphIndex == 0 {
		fmt.Fprintf(os.Stderr, "WARNING: glyph index is 0 (missing glyph) for U+275D\n")
	}
	fmt.Printf("Glyph index: %d\n", glyphIndex)

	// Try loading with various ppem values
	// ppem=1000 gives scaled coordinates in font units
	ppem := fixed.Int26_6(1000 * 64) // 1000px em size
	segments, err := font.LoadGlyph(buf, glyphIndex, ppem, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot load glyph: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Number of segments: %d\n", len(segments))

	// Print first few segments for debugging
	for i, seg := range segments {
		if i >= 5 {
			break
		}
		opStr := "?"
		switch seg.Op {
		case sfnt.SegmentOpMoveTo:
			opStr = "M"
		case sfnt.SegmentOpLineTo:
			opStr = "L"
		case sfnt.SegmentOpQuadTo:
			opStr = "Q"
		case sfnt.SegmentOpCubeTo:
			opStr = "C"
		}
		fmt.Printf("  seg[%d]: op=%s args=%v\n", i, opStr, seg.Args)
	}

	if len(segments) == 0 {
		fmt.Fprintf(os.Stderr, "ERROR: glyph has no outline segments\n")
		os.Exit(1)
	}

	svgPath, minX, minY, maxX, maxY := segmentsToSVGPath(segments)
	fmt.Printf("Glyph bbox: x=[%d, %d] y=[%d, %d]\n", f2i(minX), f2i(maxX), f2i(minY), f2i(maxY))

	// Add generous padding for this specific glyph
	padding := fixed.Int26_6(200)
	svgW := int(math.Ceil(float64(maxX-minX+padding*2) / 64))
	svgH := int(math.Ceil(float64(maxY-minY+padding*2) / 64))

	// Y-shift upward
	yShift := fixed.Int26_6(-300)

	viewBox := fmt.Sprintf("%d %d %d %d",
		int(math.Floor(float64(minX-padding)/64)),
		int(math.Floor(float64(minY-padding+yShift)/64)),
		svgW, svgH)

	svg := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="%s" width="%d" height="%d">
  <path d="%s" fill="black"/>
</svg>`, viewBox, svgW, svgH, svgPath)

	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot create output directory: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(outPath, []byte(svg), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot write SVG: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("SVG written to %s (%dx%d)\n", outPath, svgW, svgH)
}

func segmentsToSVGPath(segments []sfnt.Segment) (path string, minX, minY, maxX, maxY fixed.Int26_6) {
	var sb strings.Builder
	setFirst := false

	for _, seg := range segments {
		switch seg.Op {
		case sfnt.SegmentOpMoveTo:
			if len(seg.Args) < 1 {
				continue
			}
			x, y := seg.Args[0].X, seg.Args[0].Y
			sb.WriteString(fmt.Sprintf("M%d %d ", f2i(x), f2i(y)))
			if !setFirst {
				updateBounds(&minX, &minY, &maxX, &maxY, x, y, true)
				setFirst = true
			} else {
				updateBounds(&minX, &minY, &maxX, &maxY, x, y, false)
			}

		case sfnt.SegmentOpLineTo:
			if len(seg.Args) < 1 {
				continue
			}
			x, y := seg.Args[0].X, seg.Args[0].Y
			sb.WriteString(fmt.Sprintf("L%d %d ", f2i(x), f2i(y)))
			updateBounds(&minX, &minY, &maxX, &maxY, x, y, !setFirst)
			setFirst = true

		case sfnt.SegmentOpQuadTo:
			if len(seg.Args) < 2 {
				continue
			}
			x1, y1 := seg.Args[0].X, seg.Args[0].Y
			x2, y2 := seg.Args[1].X, seg.Args[1].Y
			sb.WriteString(fmt.Sprintf("Q%d %d %d %d ", f2i(x1), f2i(y1), f2i(x2), f2i(y2)))
			updateBounds(&minX, &minY, &maxX, &maxY, x1, y1, !setFirst)
			updateBounds(&minX, &minY, &maxX, &maxY, x2, y2, false)
			setFirst = true

		case sfnt.SegmentOpCubeTo:
			if len(seg.Args) < 3 {
				continue
			}
			x1, y1 := seg.Args[0].X, seg.Args[0].Y
			x2, y2 := seg.Args[1].X, seg.Args[1].Y
			x3, y3 := seg.Args[2].X, seg.Args[2].Y
			sb.WriteString(fmt.Sprintf("C%d %d %d %d %d %d ", f2i(x1), f2i(y1), f2i(x2), f2i(y2), f2i(x3), f2i(y3)))
			updateBounds(&minX, &minY, &maxX, &maxY, x1, y1, !setFirst)
			updateBounds(&minX, &minY, &maxX, &maxY, x2, y2, false)
			updateBounds(&minX, &minY, &maxX, &maxY, x3, y3, false)
			setFirst = true
		}
	}

	path = strings.TrimSpace(sb.String())
	return
}

func f2i(x fixed.Int26_6) int {
	return int(math.Round(float64(x) / 64))
}

func updateBounds(minX, minY, maxX, maxY *fixed.Int26_6, x, y fixed.Int26_6, first bool) {
	if first || x < *minX {
		*minX = x
	}
	if first || y < *minY {
		*minY = y
	}
	if first || x > *maxX {
		*maxX = x
	}
	if first || y > *maxY {
		*maxY = y
	}
}

func findFile(relPath string) string {
	dir, _ := os.Getwd()
	for {
		candidate := filepath.Join(dir, relPath)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fmt.Fprintf(os.Stderr, "ERROR: cannot find %s from any parent directory\n", relPath)
			os.Exit(1)
		}
		dir = parent
	}
}

func findBase() string {
	dir, _ := os.Getwd()
	for {
		candidate := filepath.Join(dir, "frontend")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			fmt.Fprintf(os.Stderr, "ERROR: cannot find project root (frontend/ directory)\n")
			os.Exit(1)
		}
		dir = parent
	}
}
