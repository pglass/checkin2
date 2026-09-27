package qr

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPayloadRoundTrip(t *testing.T) {
	p := NewPayload("John", "Smith")
	b, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"Version":2,"FirstName":"John","LastName":"Smith"}` {
		t.Fatalf("unexpected JSON: %s", b)
	}
	got, err := ParsePayload(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 || got.FirstName != "John" || got.LastName != "Smith" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

// A v1 payload carries a single joined Name, which cannot be split back into
// two columns; it parses but reports the old version, and the scan path
// rejects it on the version check.
func TestV1PayloadIsNotCurrentVersion(t *testing.T) {
	got, err := ParsePayload(`{"Version":1,"Name":"John Smith"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version == Version {
		t.Error("a v1 payload must not pass as the current version")
	}
	if got.FirstName != "" || got.LastName != "" {
		t.Errorf("v1 payload yielded name parts: %+v", got)
	}
}

func testStudent(first, last string) Student {
	return Student{First: first, Last: last, Label: last + ", " + first}
}

func TestGenerateImages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "QR Codes", "Center A") // not yet created
	students := []Student{
		testStudent("John", "Smith"),
		testStudent("Jane", "Doe"),
	}
	paths, err := GenerateImages(students, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(students) {
		t.Fatalf("got %d paths, want %d", len(paths), len(students))
	}
	for i, p := range paths {
		if filepath.Dir(p) != dir {
			t.Errorf("path %d = %s, want it inside %s", i, p, dir)
		}
		if got, want := filepath.Base(p), students[i].Label+".png"; got != want {
			t.Errorf("path %d is named %q, want %q", i, got, want)
		}
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := png.DecodeConfig(f)
		f.Close()
		if err != nil {
			t.Fatalf("%s is not a readable PNG: %v", p, err)
		}
		if cfg.Width != CardWidth || cfg.Height != CardHeight {
			t.Errorf("%s is %dx%d, want %dx%d", p, cfg.Width, cfg.Height, CardWidth, CardHeight)
		}
	}
	// No leftover temporary files from the write-then-rename.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(students) {
		t.Errorf("directory holds %d files, want %d", len(entries), len(students))
	}
}

// Students with the same name still get one file each, rather than one
// overwriting the other.
func TestGenerateImagesDistinctFilesForDuplicateNames(t *testing.T) {
	dir := t.TempDir()
	paths, err := GenerateImages([]Student{
		testStudent("John", "Smith"),
		testStudent("John", "Smith"),
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if paths[0] == paths[1] {
		t.Fatalf("both students wrote to %s", paths[0])
	}
	// The first of the pair keeps the plain name; only the clash is numbered.
	if got, want := filepath.Base(paths[0]), "Smith, John.png"; got != want {
		t.Errorf("first file is named %q, want %q", got, want)
	}
	if got, want := filepath.Base(paths[1]), "Smith, John-2.png"; got != want {
		t.Errorf("second file is named %q, want %q", got, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("directory holds %d files, want 2", len(entries))
	}
}

func TestGenerateImagesProgressReportsEveryStudent(t *testing.T) {
	students := make([]Student, 7)
	for i := range students {
		students[i] = testStudent("Student", "Family "+string(rune('A'+i)))
	}
	var seen int
	last := -1
	if _, err := GenerateImagesProgress(students, t.TempDir(), func(done, total int) {
		seen++
		if total != len(students) {
			t.Errorf("progress total = %d, want %d", total, len(students))
		}
		if done > last {
			last = done
		}
	}); err != nil {
		t.Fatal(err)
	}
	if seen != len(students) {
		t.Errorf("progress called %d times, want %d", seen, len(students))
	}
	if last != len(students) {
		t.Errorf("progress finished at %d, want %d", last, len(students))
	}
}

// The card's geometry is fixed: white background, the code in a 480x480 box
// centered horizontally 60px from the top, and dark ink below it for the name.
func TestCardLayout(t *testing.T) {
	img, err := Card(testStudent("John", "Smith"))
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Dx() != CardWidth || b.Dy() != CardHeight {
		t.Fatalf("card is %dx%d, want %dx%d", b.Dx(), b.Dy(), CardWidth, CardHeight)
	}

	isWhite := func(x, y int) bool {
		r, g, bl, _ := img.At(x, y).RGBA()
		return r == 0xffff && g == 0xffff && bl == 0xffff
	}
	// The strip above the code, and the left/right gutters beside it, are blank.
	for y := 0; y < qrTop; y++ {
		for x := 0; x < CardWidth; x++ {
			if !isWhite(x, y) {
				t.Fatalf("ink at (%d,%d), above the code's %dpx top margin", x, y, qrTop)
			}
		}
	}
	left := (CardWidth - qrSide) / 2
	for y := qrTop; y < qrTop+qrSide; y++ {
		for x := 0; x < left; x++ {
			if !isWhite(x, y) || !isWhite(CardWidth-1-x, y) {
				t.Fatalf("ink in the gutter at row %d, outside the %dpx code", y, qrSide)
			}
		}
	}
	// The code's quiet zone is white, but the code itself must have dark pixels.
	if !hasDarkPixel(img, image.Rect(left, qrTop, left+qrSide, qrTop+qrSide)) {
		t.Error("no dark pixels inside the QR code box")
	}
	// And the name is drawn under it.
	if !hasDarkPixel(img, image.Rect(0, qrTop+qrSide, CardWidth, CardHeight)) {
		t.Error("no dark pixels below the QR code: the name is missing")
	}
}

// The QR code must survive as hard pixels: the caption is antialiased, but
// nothing in that path may bleed grey into the code, which scanners read as
// ambiguous modules.
func TestQRAreaIsPureBlackAndWhite(t *testing.T) {
	img, err := Card(testStudent("Shannaf", "Alamin"))
	if err != nil {
		t.Fatal(err)
	}
	left := (CardWidth - qrSide) / 2
	for y := qrTop; y < qrTop+qrSide; y++ {
		for x := left; x < left+qrSide; x++ {
			g := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			if g.Y != 0 && g.Y != 0xff {
				t.Fatalf("grey pixel %d at (%d,%d) inside the QR code", g.Y, x, y)
			}
		}
	}
}

// The caption, by contrast, is antialiased: its edges carry intermediate
// greys, which is what keeps the small text from looking jagged.
func TestNameIsAntialiased(t *testing.T) {
	img, err := Card(testStudent("Shannaf", "Alamin"))
	if err != nil {
		t.Fatal(err)
	}
	for y := qrTop + qrSide; y < CardHeight; y++ {
		for x := 0; x < CardWidth; x++ {
			g := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			if g.Y != 0 && g.Y != 0xff {
				return
			}
		}
	}
	t.Error("no partial-coverage pixels in the caption: the text is not antialiased")
}

// A very long name is shrunk to fit rather than running off the card.
func TestCardLongNameStaysInsideTheCard(t *testing.T) {
	long := strings.Repeat("Wolfeschlegelstein", 3) + ", Hubert"
	img, err := Card(Student{First: "Hubert", Last: "Wolfeschlegelstein", Label: long})
	if err != nil {
		t.Fatal(err)
	}
	for y := qrTop + qrSide; y < CardHeight; y++ {
		for _, x := range []int{0, CardWidth - 1} {
			r, g, b, _ := img.At(x, y).RGBA()
			if r != 0xffff || g != 0xffff || b != 0xffff {
				t.Fatalf("name ink reaches the card edge at (%d,%d)", x, y)
			}
		}
	}
}

func TestFileBaseSanitizesPathSeparators(t *testing.T) {
	got := fileBase(Student{Label: `O/Brien\Jr: "Bo" <x>`})
	if strings.ContainsAny(got, `/\:*?"<>|`) {
		t.Errorf("fileBase kept a reserved character: %q", got)
	}
	if got == "" {
		t.Error("fileBase returned an empty name")
	}
}

func TestFileBaseFallsBackWhenNameIsUnusable(t *testing.T) {
	if got := fileBase(Student{Label: "..."}); got != "student" {
		t.Errorf("fileBase of a dots-only name = %q, want \"student\"", got)
	}
	if got := fileBase(Student{}); got != "student" {
		t.Errorf("fileBase of an empty student = %q, want \"student\"", got)
	}
}

// hasDarkPixel reports whether any pixel in r is substantially darker than white.
func hasDarkPixel(img image.Image, r image.Rectangle) bool {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := color.GrayModel.Convert(img.At(x, y)).(color.Gray)
			if c.Y < 128 {
				return true
			}
		}
	}
	return false
}
