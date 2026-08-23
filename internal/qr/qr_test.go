package qr

import (
	"os"
	"path/filepath"
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

func TestGeneratePDF(t *testing.T) {
	students := make([]Student, 25) // spans multiple pages (12/page)
	for i := range students {
		last := "Family " + string(rune('A'+i%26))
		students[i] = Student{
			First: "Student",
			Last:  last,
			Label: last + ", Student",
		}
	}
	path := filepath.Join(t.TempDir(), "codes.pdf")
	if err := GeneratePDF(students, path); err != nil {
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
