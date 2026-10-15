package favoriten

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func store(t *testing.T, content string) *Store {
	t.Helper()
	dir := t.TempDir()
	if content != "" {
		if err := os.WriteFile(filepath.Join(dir, File), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Open(dir)
}

// laib ADR 0017: favorites go to Haus, one list per person (Authelia's
// Remote-User), in the person's order; another person's list is not theirs.
func TestForIsThePersonsListInOrder(t *testing.T) {
	s := store(t, `{"anna": ["cover.wohnzimmer", "light.buero", "switch.kaffee"], "ben": ["light.flur"]}`)
	got, err := s.For("anna")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"cover.wohnzimmer", "light.buero", "switch.kaffee"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("anna: %v, want %v", got, want)
	}
	got, err = s.For("ben")
	if err != nil || !reflect.DeepEqual(got, []string{"light.flur"}) {
		t.Fatalf("ben: %v %v", got, err)
	}
	for _, nobody := range []string{"", "carla", "Anna", " anna"} {
		if got, err := s.For(nobody); err != nil || len(got) != 0 {
			t.Fatalf("%q: %v %v", nobody, got, err)
		}
	}
}

// No file yet is no favorites, not an error: a fresh box has none.
func TestNoFileIsNoFavorites(t *testing.T) {
	got, err := store(t, "").For("anna")
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}

// Only entity ids are favorites; the rest of a hand-edited file is left
// out (a padded id too: Haus does not guess), each id once.
func TestForKeepsOnlyEntityIDsOnce(t *testing.T) {
	s := store(t, `{"anna": ["light.buero", " light.bad ", "light.buero", "light.flur", "Light.Bad", "light", ".x", "light.a b", "", "../etc/passwd", "light.büro", "cover.tor"]}`)
	got, err := s.For("anna")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"light.buero", "light.flur", "cover.tor"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%v, want %v", got, want)
	}
}

// A file that does not read is an error the page can say plainly; it is
// never taken as "no favorites".
func TestABrokenFileIsAnError(t *testing.T) {
	for _, content := range []string{`{"anna": [`, `["light.buero"]`, `{"anna": "light.buero"}`, `{"anna": [1, 2]}`} {
		if got, err := store(t, content).For("anna"); err == nil {
			t.Errorf("%s: %v, want an error", content, got)
		}
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, File), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir).For("anna"); err == nil {
		t.Error("a folder where the file belongs read as no favorites")
	}
}

// A file far larger than any family's favorites is not read into memory.
func TestAHugeFileIsAnError(t *testing.T) {
	big := `{"anna": ["light.buero"], "x": "` + strings.Repeat("a", maxBytes) + `"}`
	if _, err := store(t, big).For("anna"); err == nil {
		t.Fatal("a file over the limit was read")
	}
	// valid JSON whose tail past the limit is only blanks is over the limit too
	padded := `{"anna": ["light.buero"]}` + strings.Repeat(" ", maxBytes)
	if _, err := store(t, padded).For("anna"); err == nil {
		t.Fatal("a file over the limit was read because its cut still parsed")
	}
}

// The file is read on each ask: a change shows on the next page load.
func TestForReadsTheFileEachTime(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	p := filepath.Join(dir, File)
	if err := os.WriteFile(p, []byte(`{"anna": ["light.buero"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.For("anna"); !reflect.DeepEqual(got, []string{"light.buero"}) {
		t.Fatalf("first: %v", got)
	}
	if err := os.WriteFile(p, []byte(`{"anna": ["light.flur"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.For("anna"); !reflect.DeepEqual(got, []string{"light.flur"}) {
		t.Fatalf("after the change: %v", got)
	}
}

// haus#35: the ☆ on a thing's own page pins it to the person's Start and
// takes it off again: at the end of their list, once, and nobody else's
// list changes. The file stays the family's alone (0600).
func TestSetPinsAndUnpins(t *testing.T) {
	dir := t.TempDir()
	s := Open(dir)
	for _, step := range []struct {
		person, id string
		on         bool
	}{{"anna", "light.buero", true}, {"anna", "cover.wohnzimmer", true}, {"ben", "light.flur", true}, {"anna", "light.buero", true}} {
		if err := s.Set(step.person, step.id, step.on); err != nil {
			t.Fatalf("%+v: %v", step, err)
		}
	}
	if got, _ := s.For("anna"); !reflect.DeepEqual(got, []string{"light.buero", "cover.wohnzimmer"}) {
		t.Fatalf("anna: %v", got)
	}
	if err := s.Set("anna", "light.buero", false); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("anna", "light.nie", false); err != nil {
		t.Fatalf("taking off what is not there: %v", err)
	}
	if got, _ := s.For("anna"); !reflect.DeepEqual(got, []string{"cover.wohnzimmer"}) {
		t.Fatalf("anna after unpinning: %v", got)
	}
	if got, _ := s.For("ben"); !reflect.DeepEqual(got, []string{"light.flur"}) {
		t.Fatalf("ben: %v", got)
	}
	fi, err := os.Stat(filepath.Join(dir, File))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the file: %v %v", fi, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 1 {
		t.Fatalf("a write left more than the file behind: %v", left)
	}
}

// What is not a person or not an entity id is refused, and a file that
// does not read is never written over: the family's lists stay as they
// were.
func TestSetRefusesAndNeverClobbers(t *testing.T) {
	s := store(t, `{"anna": ["light.buero"]}`)
	for _, c := range []struct{ person, id string }{{"", "light.buero"}, {"anna", "Light.Buero"}, {"anna", "../x"}, {"anna", ""}} {
		if err := s.Set(c.person, c.id, true); err == nil {
			t.Errorf("%q %q: pinned", c.person, c.id)
		}
	}
	if got, _ := s.For("anna"); !reflect.DeepEqual(got, []string{"light.buero"}) {
		t.Fatalf("anna: %v", got)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, File)
	broken := `{"anna": ["light.buero"`
	if err := os.WriteFile(p, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Open(dir).Set("anna", "light.flur", true); err == nil {
		t.Fatal("pinned into a file that does not read")
	}
	if b, _ := os.ReadFile(p); string(b) != broken {
		t.Fatalf("the broken file was written over: %q", b)
	}
}

// A person's list has an end: past MaxPerPerson, pinning says so.
func TestSetHasAnEnd(t *testing.T) {
	s := Open(t.TempDir())
	for i := 0; i < MaxPerPerson; i++ {
		if err := s.Set("anna", "light.l"+strconv.Itoa(i), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Set("anna", "light.zuviel", true); !errors.Is(err, ErrFull) {
		t.Fatalf("%v, want ErrFull", err)
	}
	if err := s.Set("anna", "light.l0", true); err != nil {
		t.Fatalf("pinning what is there again: %v", err)
	}
}
