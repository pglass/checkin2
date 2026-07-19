package qr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPayloadRoundTrip(t *testing.T) {
	p := NewPayload("John Smith")
	b, err := p.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"Version":1,"Name":"John Smith"}` {
		t.Fatalf("unexpected JSON: %s", b)
	}
	got, err := ParsePayload(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.Name != "John Smith" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestGeneratePDF(t *testing.T) {
	names := make([]string, 25) // spans multiple pages (12/page)
	for i := range names {
		names[i] = "Student " + string(rune('A'+i%26))
	}
	path := filepath.Join(t.TempDir(), "codes.pdf")
	if err := GeneratePDF(names, path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() < 1000 {
		t.Fatalf("PDF suspiciously small: %d bytes", fi.Size())
	}
}
