package persona

import (
	"os"
	"path/filepath"
	"testing"
)

func fake(t *testing.T) string {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "joshua"), 0o755)
	os.WriteFile(filepath.Join(dir, "joshua", "persona.json"), []byte(`{"name":"joshua","display":"Joshua","greeting":{"de":"Hallo, hier ist Joshua."},"colors":{"accent":"#3ddc84","ground":"#0c120e"},"languages":["de","en"]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "joshua", "icon.svg"), []byte("<svg/>"), 0o644)
	return dir
}

// laib#129: the house persona laib rendered to /persona/<name>/persona.json.
func TestLoadTheHousePersona(t *testing.T) {
	p := Load(fake(t), "joshua")
	if p.Display != "Joshua" || p.Greet("de") != "Hallo, hier ist Joshua." || p.Accent() != "#3ddc84" || string(p.Icon) != "<svg/>" {
		t.Fatalf("persona: %+v", p)
	}
}

// Without the mount or with a broken file Haus is plain Haus, never down.
func TestWithoutAPersonaHausIsHaus(t *testing.T) {
	for _, p := range []Persona{Load(t.TempDir(), "joshua"), Load(fake(t), ""), Load(fake(t), "../etc")} {
		if p.Display != "Haus" || p.Greet("de") != "" || p.Accent() == "" || p.Ground() == "" {
			t.Fatalf("default persona: %+v", p)
		}
	}
}
