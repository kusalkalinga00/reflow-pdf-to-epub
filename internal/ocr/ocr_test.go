package ocr

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/png"
	"strings"
	"testing"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const sampleTSV = `level	page_num	block_num	par_num	line_num	word_num	left	top	width	height	conf	text
1	1	0	0	0	0	0	0	600	200	-1	
2	1	1	0	0	0	40	30	520	40	-1	
3	1	1	1	0	0	40	30	520	40	-1	
4	1	1	1	1	0	40	30	520	40	-1	
5	1	1	1	1	1	40	30	200	40	96	Hello
5	1	1	1	1	2	260	30	300	40	88	world
4	1	1	1	2	0	40	90	200	40	-1	
5	1	1	1	2	1	40	90	120	40	92	Second
5	1	1	1	2	2	170	90	390	40	91	line
`

func TestParseTSVGroupsWordsIntoLines(t *testing.T) {
	lines, err := parseTSV([]byte(sampleTSV))
	if err != nil {
		t.Fatalf("parseTSV: %v", err)
	}

	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %+v", len(lines), lines)
	}

	first := lines[0]
	if first.Text != "Hello world" {
		t.Errorf("text = %q", first.Text)
	}
	if first.Left != 40 || first.Top != 30 || first.Right != 560 || first.Bottom != 70 {
		t.Errorf("box = %+v, want the union of its words", first)
	}
	if first.Confidence < 0.9 || first.Confidence > 0.93 {
		t.Errorf("confidence = %v, want about 0.92", first.Confidence)
	}

	if lines[1].Text != "Second line" {
		t.Errorf("second line = %q", lines[1].Text)
	}
}

func TestParseTSVSkipsNonWordRowsAndEmptyText(t *testing.T) {
	tsv := `level	page_num	block_num	par_num	line_num	word_num	left	top	width	height	conf	text
1	1	0	0	0	0	0	0	10	10	-1	
5	1	1	1	1	1	10	10	5	5	-1	noise
5	1	1	1	1	2	10	10	5	5	80	
`
	lines, err := parseTSV([]byte(tsv))
	if err != nil {
		t.Fatalf("parseTSV: %v", err)
	}
	if len(lines) != 0 {
		t.Fatalf("got %d lines, want 0: %+v", len(lines), lines)
	}
}

func TestParseTSVEmpty(t *testing.T) {
	lines, err := parseTSV(nil)
	if err != nil {
		t.Fatalf("parseTSV: %v", err)
	}
	if len(lines) != 0 {
		t.Errorf("got %d lines, want 0", len(lines))
	}
}

// buildImage draws text with a bitmap face and scales it up, which is enough
// for a recognizer test and needs no external assets.
func buildImage(t *testing.T, text string) []byte {
	t.Helper()

	const scale = 6

	small := image.NewRGBA(image.Rect(0, 0, 200, 40))
	draw.Draw(small, small.Bounds(), image.White, image.Point{}, draw.Src)

	drawer := &font.Drawer{
		Dst:  small,
		Src:  image.Black,
		Face: basicfont.Face7x13,
		Dot:  fixed.P(8, 26),
	}
	drawer.DrawString(text)

	big := image.NewRGBA(image.Rect(0, 0, 200*scale, 40*scale))
	for y := 0; y < big.Bounds().Dy(); y++ {
		for x := 0; x < big.Bounds().Dx(); x++ {
			big.Set(x, y, small.At(x/scale, y/scale))
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, big); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestTesseractRecognizesText(t *testing.T) {
	if !Available() {
		t.Skip("tesseract is not installed")
	}

	recognizer, err := NewTesseract()
	if err != nil {
		t.Fatalf("NewTesseract: %v", err)
	}

	lines, err := recognizer.Recognize(context.Background(), buildImage(t, "HELLO WORLD"))
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("no lines were recognized")
	}

	var text strings.Builder
	for _, l := range lines {
		text.WriteString(l.Text)
		text.WriteString(" ")
	}
	if !strings.Contains(strings.ToUpper(text.String()), "HELLO") {
		t.Errorf("recognized %q, want it to contain HELLO", text.String())
	}
	if lines[0].Confidence <= 0 {
		t.Errorf("confidence = %v, want a positive value", lines[0].Confidence)
	}
}

func TestNewTesseractMissingBinary(t *testing.T) {
	t.Setenv("PATH", "/nonexistent")

	if Available() {
		t.Fatal("Available() = true with an empty PATH")
	}
	if _, err := NewTesseract(); err == nil {
		t.Fatal("NewTesseract() = nil error without a binary")
	}
}

func TestLooksLikeText(t *testing.T) {
	cover := []Line{
		{Text: "|"}, {Text: "i"}, {Text: "a"}, {Text: "ad"},
		{Text: "—"}, {Text: "="}, {Text: "="}, {Text: "Fi"},
	}
	if LooksLikeText(cover) {
		t.Error("cover noise was accepted as a text page")
	}

	prose := []Line{{Text: "It was the best of times, it was the worst of times."}}
	if !LooksLikeText(prose) {
		t.Error("a line of prose was rejected")
	}

	if LooksLikeText(nil) {
		t.Error("no lines were accepted as text")
	}
}
