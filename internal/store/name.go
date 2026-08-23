package store

import "strings"

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

// Less orders names by (Last, First), case-insensitively, which is the sort
// order used everywhere students are listed. Case-insensitive so "de Vries"
// files next to "De Vries" rather than after every capitalised surname.
func (n Name) Less(o Name) bool {
	if a, b := strings.ToLower(n.Last), strings.ToLower(o.Last); a != b {
		return a < b
	}
	return strings.ToLower(n.First) < strings.ToLower(o.First)
}
