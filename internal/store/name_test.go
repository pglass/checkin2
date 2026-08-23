package store

import (
	"sort"
	"testing"
)

func TestNameDisplayAndFull(t *testing.T) {
	tests := []struct {
		name          string
		n             Name
		display, full string
	}{
		{"both parts", Name{First: "John", Last: "Smith"}, "Smith, John", "John Smith"},
		{"first only", Name{First: "John"}, "John", "John"},
		{"last only", Name{Last: "Smith"}, "Smith", "Smith"},
		{"empty", Name{}, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.n.Display(); got != tc.display {
				t.Errorf("Display() = %q, want %q", got, tc.display)
			}
			if got := tc.n.Full(); got != tc.full {
				t.Errorf("Full() = %q, want %q", got, tc.full)
			}
		})
	}
}

func TestNewNameTrims(t *testing.T) {
	got := NewName("  John  ", "\tSmith\n")
	want := Name{First: "John", Last: "Smith"}
	if got != want {
		t.Errorf("NewName() = %+v, want %+v", got, want)
	}
}

// Sorting is by last name, then first, ignoring case -- so a shared surname
// falls back to the first name, and capitalisation never splits a family.
func TestNameLessOrdering(t *testing.T) {
	names := []Name{
		{First: "Carol", Last: "adams"},
		{First: "Bob", Last: "Zane"},
		{First: "alice", Last: "Adams"},
		{First: "Dave", Last: "Mills"},
	}
	sort.Slice(names, func(i, j int) bool { return names[i].Less(names[j]) })

	var got []string
	for _, n := range names {
		got = append(got, n.Display())
	}
	want := []string{"Adams, alice", "adams, Carol", "Mills, Dave", "Zane, Bob"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted = %v, want %v", got, want)
		}
	}
}

// Empty reports a name that cannot identify a student: either part missing.
func TestNameEmpty(t *testing.T) {
	tests := []struct {
		n    Name
		want bool
	}{
		{Name{First: "John", Last: "Smith"}, false},
		{Name{First: "John"}, true},
		{Name{Last: "Smith"}, true},
		{Name{}, true},
	}
	for _, tc := range tests {
		if got := tc.n.Empty(); got != tc.want {
			t.Errorf("%+v.Empty() = %v, want %v", tc.n, got, tc.want)
		}
	}
}
