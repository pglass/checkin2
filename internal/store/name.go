package store

import (
	"fmt"
	"strings"
	"unicode"
)

// nameExtraRunes are the punctuation marks a student's name may contain
// besides letters, digits, and spaces: hyphens and apostrophes (straight and
// curly) for names like "Mary-Jo O'Neil", and periods for "St. John" or "Jr.".
const nameExtraRunes = "-'’."

// nameRuneOK reports whether r may appear in a student's name: a letter or digit
// in any script, a combining mark (accents typed as separate code points), a
// space, or one of nameExtraRunes. An allowlist, so nothing that means
// something in the QR code's URL -- "+" separates the two parts there -- or in
// a file name can get in.
func nameRuneOK(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.M, r) ||
		r == ' ' || strings.ContainsRune(nameExtraRunes, r)
}

// Name is a student's name, stored as two columns and displayed two ways.
//
// The pair is the identity of a student (the Student table's UNIQUE key), so
// every comparison and every sort in the app goes through this type rather
// than re-deriving the rules from a joined string.
type Name struct {
	First string
	Last  string
}

// NewName trims both parts. Empty parts are allowed here and rejected by
// AddStudent, so callers can build a partial Name while a form is filled in.
func NewName(first, last string) Name {
	return Name{First: strings.TrimSpace(first), Last: strings.TrimSpace(last)}
}

// Display is the roster form, "Last, First" -- used by the main list, the
// student pickers, and anywhere names are read in sorted order.
func (n Name) Display() string {
	switch {
	case n.Last == "" && n.First == "":
		return ""
	case n.Last == "":
		return n.First
	case n.First == "":
		return n.Last
	}
	return n.Last + ", " + n.First
}

// Full is the natural form, "First Last" -- used in prose: dialog headings,
// history lines, and the feedback bar.
func (n Name) Full() string {
	return strings.TrimSpace(n.First + " " + n.Last)
}

// Empty reports whether either part is missing. Both are required for a
// student to be added.
func (n Name) Empty() bool { return n.First == "" || n.Last == "" }

// Validate reports whether n can be a student's name: both parts present, and
// made only of the characters nameRuneOK allows. The error is shown to the
// user as is.
func (n Name) Validate() error {
	if n.Empty() {
		return fmt.Errorf("first and last name must not be empty")
	}
	for _, part := range []string{n.First, n.Last} {
		for _, r := range part {
			if !nameRuneOK(r) {
				return fmt.Errorf("names cannot contain %q: use letters, digits, spaces, and - ' ’ .", r)
			}
		}
	}
	return nil
}

// Less orders names by (Last, First), case-insensitively, which is the sort
// order used everywhere students are listed. Case-insensitive so "de Vries"
// files next to "De Vries" rather than after every capitalised surname.
func (n Name) Less(o Name) bool {
	if a, b := strings.ToLower(n.Last), strings.ToLower(o.Last); a != b {
		return a < b
	}
	return strings.ToLower(n.First) < strings.ToLower(o.First)
}
