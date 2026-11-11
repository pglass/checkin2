package config

import (
	"reflect"
	"strconv"
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
		if f.List {
			if f.GetList == nil || f.SetList == nil {
				t.Errorf("list key %q missing GetList or SetList", f.Key)
			}
			if f.AddLabel == "" || f.RemovePrompt == "" || f.ListExample == "" {
				t.Errorf("list key %q missing AddLabel, RemovePrompt, or ListExample", f.Key)
			}
			continue
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
		if f.List {
			if err := f.SetList(&got, f.GetList(def)); err != nil {
				t.Fatalf("%s: SetList(GetList(default)) = %v", f.Key, err)
			}
			continue
		}
		if err := f.Set(&got, f.Get(def)); err != nil {
			t.Fatalf("%s: Set(Get(default)) = %v", f.Key, err)
		}
	}
	if !reflect.DeepEqual(got, def) {
		t.Fatalf("round trip = %+v, want %+v", got, def)
	}
}

// Set trims surrounding whitespace and rejects bad input per type.
func TestFieldsSetValidation(t *testing.T) {
	for _, f := range Fields {
		if f.List {
			continue // covered by TestBackupDirsSetList
		}
		cfg := Default()
		padded := "  " + f.Get(Default()) + "  "
		if err := f.Set(&cfg, padded); err != nil {
			t.Errorf("%s: Set(%q) = %v, want space to be trimmed", f.Key, padded, err)
		}
		// What counts as invalid depends on the field's type: "0" and "-1" are
		// nonsense for a positive int or duration, but "0" is a perfectly good
		// false for a bool. Key off whether the current value parses as a bool.
		// A free-text field (e.g. a directory path) has nothing to reject, and
		// "" is how such an optional setting is turned off.
		if f.FreeText {
			continue
		}
		bad := []string{"", "abc"}
		if _, err := strconv.ParseBool(f.Get(Default())); err != nil {
			bad = append(bad, "-1", "0")
		} else {
			bad = append(bad, "2", "yes")
		}
		for _, bad := range bad {
			if err := f.Set(&cfg, bad); err == nil {
				t.Errorf("%s: Set(%q) = nil, want error", f.Key, bad)
			} else if !strings.Contains(err.Error(), f.Key) {
				t.Errorf("%s: error %q should name the key", f.Key, err)
			}
		}
	}
}

// backup_dirs values are trimmed, and empty or repeated directories dropped.
func TestBackupDirsSetList(t *testing.T) {
	var c Config
	err := setBackupDirs(&c, []string{"  /a  ", "", "/b", "/a/", "  ", "/c"})
	if err != nil {
		t.Fatalf("setBackupDirs: %v", err)
	}
	if want := []string{"/a", "/b", "/c"}; !reflect.DeepEqual(c.BackupDirs, want) {
		t.Errorf("BackupDirs = %q, want %q", c.BackupDirs, want)
	}
	if err := setBackupDirs(&c, []string{"", " "}); err != nil {
		t.Fatalf("setBackupDirs: %v", err)
	}
	if c.BackupDirs != nil {
		t.Errorf("all-empty list = %q, want nil like Default", c.BackupDirs)
	}
}

func TestListFieldByKey(t *testing.T) {
	for key, want := range map[string]int{
		"backup_dirs1": 1, "backup_dirs12": 12, "BACKUP_DIRS2": 2, "backup_dirs": 0,
	} {
		f, n, ok := listFieldByKey(key)
		if !ok || f.Key != KeyBackupDirs || n != want {
			t.Errorf("listFieldByKey(%q) = %q, %d, %v; want backup_dirs, %d, true", key, f.Key, n, ok, want)
		}
	}
	for _, key := range []string{"backup_dir", "backup_dirsx", "backup_dirs-1", "backup_dirs01", "backup_dirs_1"} {
		if _, _, ok := listFieldByKey(key); ok {
			t.Errorf("listFieldByKey(%q) matched, want no match", key)
		}
	}
}
