package pages

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/einkauf"
	"github.com/mdopp/haus/internal/notes"
)

func shoppingList(t *testing.T) (*einkauf.Liste, *int) {
	t.Helper()
	dav := davfake.New("haus", "pw")
	t.Cleanup(dav.Close)
	l := einkauf.Open(&caldav.Client{URL: dav.URL, User: "haus", Password: "pw"})
	if err := l.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	told := 0
	l.Changed = func() { told++ }
	return l, &told
}

func uidOf(t *testing.T, l *einkauf.Liste, summary string) string {
	t.Helper()
	items, err := l.Lesen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Summary == summary {
			return it.UID
		}
	}
	t.Fatalf("%q not on the list: %+v", summary, items)
	return ""
}

// haus#118: the list under Haushalt — empty says how it fills; a line
// added answers on the page; a tap ticks it off (a switch, at once), the
// ticked ones go below with Abgehakte entfernen (asked first); every change
// is told to the other phones; only from Haus's own page.
func TestEinkaufPage(t *testing.T) {
	l, told := shoppingList(t)
	cfg := StartConfig{Einkauf: l}
	h := Einkauf(cfg)

	_, body, _ := familyPage(t, Einkauf, cfg, "GET", EinkaufPath, "family")
	mustContain(t, body, "<h1>Einkauf</h1>", saysEinkaufEmpty, `data-live=seite`, `<span hidden>`+EinkaufLive+`</span>`,
		`<input type=hidden name=tun value=dazu>`, `href=/einkauf aria-current=page`)
	mustNotContain(t, body, "Abgehakte entfernen", "Zutaten aus Rezepten") // no notes: no recipes

	rec := post(t, h, EinkaufPath, url.Values{"tun": {"dazu"}, "was": {"Milch, 200 g Mehl\n3 Eier"}}, "anna", nil)
	if rec.Code != 200 {
		t.Fatalf("dazu → %d %s", rec.Code, rec.Body)
	}
	body = rec.Body.String()
	mustContain(t, body, "<div class=gut role=status><span class=punkt></span><p>3 Zeilen neu auf der Liste.</p>",
		`<button class=posten role=switch aria-checked=false><span class=haken></span><span class=was>200 g Mehl</span>`)
	rec = post(t, h, EinkaufPath, url.Values{"tun": {"dazu"}, "was": {"300 g Mehl"}}, "anna", nil)
	mustContain(t, rec.Body.String(), "1 zu einer Zeile dazugezählt.", "<span class=was>500 g Mehl</span>")

	rec = post(t, h, EinkaufPath, url.Values{"tun": {"haken"}, "uid": {uidOf(t, l, "Milch")}, "erledigt": {"1"}}, "anna", nil)
	body = rec.Body.String()
	mustContain(t, body, "<p class=einkauf-zwischen>Abgehakt</p>", `aria-checked=true><span class=haken></span><span class=was>Milch</span>`,
		`data-frage="1 abgehakte Zeile von der Liste nehmen?"`, `<input type=hidden name=erledigt value=0>`)
	if strings.Index(body, ">Milch<") < strings.Index(body, ">3 Eier<") {
		t.Error("the ticked line is not below the open ones")
	}
	rec = post(t, h, EinkaufPath, url.Values{"tun": {"aufraeumen"}}, "anna", nil)
	mustContain(t, rec.Body.String(), "1 abgehakte Zeile entfernt.")
	mustNotContain(t, rec.Body.String(), ">Milch<", "Abgehakt</p>")

	// a line gone meanwhile: a sentence at the bottom, the page as it was
	rec = post(t, h, EinkaufPath, url.Values{"tun": {"haken"}, "uid": {"weg"}, "erledigt": {"1"}}, "anna", nil)
	if rec.Code != http.StatusConflict || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") || !strings.Contains(rec.Body.String(), einkauf.ErrNoLine.Error()) {
		t.Errorf("gone → %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	if rec := post(t, h, EinkaufPath, url.Values{"tun": {"dazu"}, "was": {" , "}}, "anna", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("nothing → %d", rec.Code)
	}
	if rec := post(t, h, EinkaufPath, url.Values{"tun": {"dazu"}, "was": {"Brot"}}, "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
	if *told < 5 {
		t.Errorf("the other phones were told %d times", *told)
	}
	if code, _, _ := familyPage(t, Einkauf, StartConfig{}, "GET", EinkaufPath, "family"); code != http.StatusNotFound {
		t.Errorf("without Radicale → %d", code)
	}
}

// Radicale away or slow: the page says so in a sentence and tries again by
// itself; a change says it did not save and what to do, never hangs.
func TestEinkaufWhenRadicaleFails(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer down.Close()
	cfg := StartConfig{Einkauf: einkauf.Open(&caldav.Client{URL: down.URL, User: "haus", Password: "pw"})}
	_, body, _ := familyPage(t, Einkauf, cfg, "GET", EinkaufPath, "family")
	mustContain(t, body, saysEinkaufDown, "Die Seite versucht es von selbst wieder.", "data-auffrischen=60")
	rec := post(t, Einkauf(cfg), EinkaufPath, url.Values{"tun": {"dazu"}, "was": {"Brot"}}, "anna", nil)
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != saysEinkaufWrite+"\n" {
		t.Errorf("down → %d %q", rec.Code, rec.Body)
	}

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
	}))
	defer slow.Close()
	was := einkaufWait
	einkaufWait = 50 * time.Millisecond
	defer func() { einkaufWait = was }()
	cfg = StartConfig{Einkauf: einkauf.Open(&caldav.Client{URL: slow.URL, User: "haus", Password: "pw"})}
	start := time.Now()
	rec = post(t, Einkauf(cfg), EinkaufPath, url.Values{"tun": {"dazu"}, "was": {"Brot"}}, "anna", nil)
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), saysEinkaufWrite) || time.Since(start) > time.Second {
		t.Errorf("slow → %d %q after %v", rec.Code, rec.Body, time.Since(start))
	}
}

// From recipes: choose them and the portions, see the ingredients added up
// and scaled, leave out what one has, put the rest on the list with the
// recipe as its origin; a recipe note links here.
func TestEinkaufFromRecipes(t *testing.T) {
	dir := t.TempDir()
	for name, text := range map[string]string{
		"rezepte/lasagne.md": "---\nart: rezept\ntags: [kochen]\n---\n# Lasagne\n\n*Für 4 Portionen · 1 Std.*\n\n## Zutaten\n\n- 500 g Hackfleisch\n- 2 Zwiebeln\n- 1 Dose Tomaten\n\n## Zubereitung\n\n1. Schichten.\n",
		"rezepte/curry.md":   "---\nart: rezept\n---\n# Curry\n\n*Für 2 Portionen*\n\n## Zutaten\n\n- 1 Zwiebel\n- 400 ml Kokosmilch\n",
		"einkaufsliste.md":   "# Alt\n\nkein Rezept\n",
	} {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644)
	}
	nd, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	l, _ := shoppingList(t)
	cfg := StartConfig{Einkauf: l, Notes: nd}

	_, body, _ := familyPage(t, Einkauf, cfg, "GET", EinkaufPath, "family")
	mustContain(t, body, `<a class=knopf href=/einkauf/rezepte>Zutaten aus Rezepten</a>`)
	_, body, _ = familyPage(t, Einkauf, cfg, "GET", EinkaufRezeptePath, "family")
	mustContain(t, body, `value="rezepte/lasagne.md"><span>Lasagne</span>`, `value="rezepte/curry.md"><span>Curry</span>`)
	mustNotContain(t, body, "Alt", "Auf die Einkaufsliste")

	_, body, _ = familyPage(t, Einkauf, cfg, "GET", EinkaufRezeptePath+"?n=rezepte/lasagne.md&n=rezepte/curry.md&portionen=4", "family")
	mustContain(t, body, `value="4"`, "<h2 id=k-zutaten>Zutaten</h2>", "Nimm weg, was du schon hast.",
		`value="4 Zwiebeln`+"\t"+`Lasagne, Curry"><span>4 Zwiebeln <small>Lasagne, Curry</small>`, `value="800 ml Kokosmilch`, "Auf die Einkaufsliste")

	rec := post(t, Einkauf(cfg), EinkaufPath, url.Values{"tun": {"rezepte"}, "z": {"500 g Hackfleisch\tLasagne", "4 Zwiebeln\tLasagne, Curry"}}, "anna", nil)
	mustContain(t, rec.Body.String(), "2 Zeilen neu auf der Liste.", "<span class=was>4 Zwiebeln<small>Lasagne, Curry</small>")
	if rec := post(t, Einkauf(cfg), EinkaufPath, url.Values{"tun": {"rezepte"}}, "anna", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("none chosen → %d", rec.Code)
	}

	// one recipe alone: its own portions, the recipes' names not repeated
	_, body, _ = familyPage(t, Einkauf, cfg, "GET", RezeptAufDieListe("rezepte/lasagne.md"), "family")
	mustContain(t, body, `value="4"`, `<span>500 g Hackfleisch</span>`)
	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen/rezepte/lasagne.md", "family")
	mustContain(t, body, `<a class=knopf href="/einkauf/rezepte?n=rezepte%2Flasagne.md">Auf die Einkaufsliste</a>`)
	_, body, _ = familyPage(t, Notizen, cfg, "GET", "/notizen/einkaufsliste.md", "family")
	mustNotContain(t, body, "Auf die Einkaufsliste")
}
