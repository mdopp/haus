package merken

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mdopp/haus/internal/notes"
)

func TestSchluessel(t *testing.T) {
	same := [][]string{
		{"https://youtube.com/watch?v=Ca7RfL4m744&is=vEk6", "https://www.youtube.com/watch?v=Ca7RfL4m744", "https://youtu.be/Ca7RfL4m744?si=abc", "https://m.youtube.com/watch?v=Ca7RfL4m744&feature=share", "https://music.youtube.com/watch?v=Ca7RfL4m744"},
		{"https://www.chefkoch.de/rezepte/745721177147257/Lasagne.html?utm_source=app&utm_medium=share", "http://chefkoch.de/rezepte/745721177147257/Lasagne.html#kommentare"},
		{"https://lagedernation.org/", "https://lagedernation.org"},
		{"https://open.spotify.com/intl-de/track/4uLU6?si=x", "https://open.spotify.com/track/4uLU6"},
		{"https://shop.de/a?b=2&a=1", "https://shop.de/a?a=1&b=2&fbclid=x"},
	}
	for _, group := range same {
		for _, l := range group[1:] {
			if Schluessel(l) != Schluessel(group[0]) {
				t.Errorf("%s ≠ %s: %q %q", l, group[0], Schluessel(l), Schluessel(group[0]))
			}
		}
	}
	if Schluessel("https://shop.de/a?id=1") == Schluessel("https://shop.de/a?id=2") {
		t.Error("two products are one")
	}
}

// owner 2026-10-26: the same thing shared twice is not kept twice; Haus
// says so, and brings the note up to date when the page changed — unless
// someone edited the note.
func TestMerkenTwice(t *testing.T) {
	var mu sync.Mutex
	zutaten := `"1 Hokkaido","1 l Brühe"`
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		z := zutaten
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(`<html><head><title>Kürbissuppe</title><script type="application/ld+json">{"@type":"Recipe","name":"Kürbissuppe","recipeIngredient":[` + z + `],"recipeInstructions":["Kochen."]}</script></head></html>`))
	}))
	defer site.Close()
	dir := t.TempDir()
	d, _ := notes.Open(dir)
	f := Fetcher{AllowPrivate: true}
	ctx := context.Background()
	first, err := Merken(ctx, f, nil, d, site.URL+"/suppe?utm_source=app", "")
	if err != nil || first.Schon {
		t.Fatalf("%+v %v", first, err)
	}
	again, _ := Merken(ctx, f, nil, d, site.URL+"/suppe/?fbclid=1", "#herbst")
	if !again.Schon || again.Name != first.Name || again.Kurz != "Das ist schon gemerkt: „Kürbissuppe“. Die Seite hat sich seither nicht geändert. Tags dazu: #herbst." {
		t.Fatalf("again: %+v", again)
	}
	n, _ := d.Read(ctx, first.Name)
	if k, _ := notes.SplitKopf(n.Text); !k.Has([]string{"herbst", "kochen"}) || k.Stand == "" || k.Inhalt == "" {
		t.Errorf("head %+v", k)
	}
	// the page changed, the note untouched: made again
	mu.Lock()
	zutaten = `"1 Hokkaido","1 l Brühe","200 ml Kokosmilch","1 Ingwer"`
	mu.Unlock()
	up, _ := Merken(ctx, f, nil, d, site.URL+"/suppe", "")
	if !up.Aktualisiert || up.Kurz != "Das hattest du schon gemerkt: „Kürbissuppe“. Die Seite hat sich geändert, ich habe die Notiz auf den neuen Stand gebracht: 2 Zutaten neu." {
		t.Fatalf("update: %+v", up)
	}
	n, _ = d.Read(ctx, first.Name)
	if !strings.Contains(n.Text, "- 200 ml Kokosmilch") || !strings.Contains(n.Text, "herbst") {
		t.Errorf("not up to date:\n%s", n.Text)
	}
	// someone edits the note, the page changes again: the note stays
	os.WriteFile(filepath.Join(dir, first.Name), []byte(strings.Replace(n.Text, "Kochen.", "Kochen, aber mit Chili!", 1)), 0o644)
	mu.Lock()
	zutaten = `"2 Hokkaido"`
	mu.Unlock()
	kept, _ := Merken(ctx, f, nil, d, site.URL+"/suppe", "")
	if kept.Aktualisiert || !strings.Contains(kept.Kurz, "die Notiz hast du selbst bearbeitet, darum lasse ich sie, wie sie ist") {
		t.Fatalf("edited: %+v", kept)
	}
	if n, _ := d.Read(ctx, first.Name); !strings.Contains(n.Text, "mit Chili") {
		t.Error("the edit was overwritten")
	}
	if all, _ := d.Recent(ctx, 10); len(all) != 1 {
		t.Errorf("%d notes", len(all))
	}
}
