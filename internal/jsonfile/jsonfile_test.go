package jsonfile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveReplacesWholeAndLeavesNothingBeside(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "favoriten.json")
	if err := os.WriteFile(p, []byte(`{"alt":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Save(p, map[string][]string{"anna": {"light.flur"}}); err != nil {
		t.Fatal(err)
	}
	var got map[string][]string
	b, _ := os.ReadFile(p)
	if err := json.Unmarshal(b, &got); err != nil || got["anna"][0] != "light.flur" {
		t.Fatalf("%s %v", b, err)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v, want 0600", fi.Mode().Perm())
	}
	if es, _ := os.ReadDir(dir); len(es) != 1 {
		t.Errorf("left beside it: %v", es)
	}
}

func TestSaveFailsWithoutTouchingTheFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.json")
	os.WriteFile(p, []byte(`{"alt":true}`), 0o600)
	if err := Save(p, map[string]any{"f": func() {}}); err == nil {
		t.Fatal("a value JSON cannot hold was saved")
	}
	if b, _ := os.ReadFile(p); string(b) != `{"alt":true}` {
		t.Errorf("the old file changed: %s", b)
	}
	if err := Save(filepath.Join(dir, "fehlt", "x.json"), 1); err == nil {
		t.Error("a missing folder was no error")
	}
}
