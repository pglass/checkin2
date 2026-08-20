package config

import (
	"reflect"
	"strings"
	"testing"
)

// TestFieldsCoverAllConfigFields is the guardrail behind the Fields table: a
// setting added to Config but not to Fields would silently be missing from the
// settings file and the Settings window. This fails until the entry is added.
func TestFieldsCoverAllConfigFields(t *testing.T) {
	covered := map[string]bool{}
	for _, f := range Fields {
		covered[f.StructField] = true
	}
	typ := reflect.TypeOf(Config{})
	for i := range typ.NumField() {
		name := typ.Field(i).Name
		if !covered[name] {
			t.Errorf("Config field %s has no entry in Fields (add one in fields.go "+
				"so it appears in settings.ini and the Settings window)", name)
		}
	}
}

// Every Fields entry must name a real Config field, so the coverage check above
// cannot be satisfied by a typo'd StructField.
func TestFieldsReferenceRealStructFields(t *testing.T) {
	typ := reflect.TypeOf(Config{})
	for _, f := range Fields {
		if _, ok := typ.FieldByName(f.StructField); !ok {
			t.Errorf("field %q references unknown Config field %q", f.Key, f.StructField)
		}
	}
}

// Keys must be unique, lowercase, and carry a description (the UI renders Desc
// under every input, and Write emits it as a comment).
func TestFieldsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range Fields {
		if f.Key != strings.ToLower(f.Key) {
			t.Errorf("key %q must be lowercase", f.Key)
		}
		if seen[f.Key] {
			t.Errorf("duplicate key %q", f.Key)
		}
		seen[f.Key] = true
		if strings.TrimSpace(f.Desc) == "" {
			t.Errorf("key %q has no description", f.Key)
		}
		if f.Section == "" {
			t.Errorf("key %q has no section", f.Key)
		}
		if f.Get == nil || f.Set == nil {
			t.Errorf("key %q missing Get or Set", f.Key)
		}
	}
}

// Round-trip: formatting a value and parsing it back yields the same Config.
func TestFieldsRoundTrip(t *testing.T) {
	def := Default()
	var got Config
	for _, f := range Fields {
		if err := f.Set(&got, f.Get(def)); err != nil {
			t.Fatalf("%s: Set(Get(default)) = %v", f.Key, err)
		}
	}
	if got != def {
		t.Fatalf("round trip = %+v, want %+v", got, def)
	}
}

// Set trims surrounding whitespace and rejects bad input per type.
func TestFieldsSetValidation(t *testing.T) {
	for _, f := range Fields {
		cfg := Default()
		padded := "  " + f.Get(Default()) + "  "
		if err := f.Set(&cfg, padded); err != nil {
			t.Errorf("%s: Set(%q) = %v, want space to be trimmed", f.Key, padded, err)
		}
		for _, bad := range []string{"", "abc", "-1", "0"} {
			if err := f.Set(&cfg, bad); err == nil {
				t.Errorf("%s: Set(%q) = nil, want error", f.Key, bad)
			} else if !strings.Contains(err.Error(), f.Key) {
				t.Errorf("%s: error %q should name the key", f.Key, err)
			}
		}
	}
}

// The prune settings are the advanced ones; everything else is shown by
// default. Advanced is a UI concern only -- Write must still emit every field.
func TestAdvancedFields(t *testing.T) {
	want := map[string]bool{"prune_interval": true, "prune_batch_size": true}
	for _, f := range Fields {
		if f.Advanced != want[f.Key] {
			t.Errorf("%s: Advanced = %v, want %v", f.Key, f.Advanced, want[f.Key])
		}
	}
}
