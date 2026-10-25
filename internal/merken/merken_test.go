package merken

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/judge"
	"github.com/mdopp/haus/internal/judge/proxyfake"
	"github.com/mdopp/haus/internal/notes"
)

const rezeptSeite = `<!doctype html><html><head><title>Lasagne | Chefkoch</title>
<meta property="og:site_name" content="Chefkoch"><meta name="description" content="Klassische Lasagne">
<script type="application/ld+json">{"@context":"https://schema.org","@type":"Recipe","name":"Lasagne al forno",
"recipeYield":"4","totalTime":"PT1H30M","recipeIngredient":["500 g Hackfleisch","1 Pck. Lasagneplatten","200 g Käse, gerieben"],
"recipeInstructions":[{"@type":"HowToStep","text":"Hackfleisch anbraten."},{"@type":"HowToStep","text":"Schichten &amp; backen."}]}</script>
</head><body><nav>Menü Menü Menü Menü Menü Menü Menü Menü Menü Menü</nav><article><p>Ein langer Text über Lasagne, der hier nicht in die Notiz muss, weil das Rezept alles sagt.</p></article></body></html>`

const albumSeite = `<html><head><meta property="og:title" content="Kettcar - Gute Laune ungerecht verteilt | VISIONS.de">
<meta property="og:site_name" content="VISIONS.de">
<script type="application/ld+json">[{"@type":"WebSite","name":"VISIONS"},{"@type":"Review","reviewRating":{"ratingValue":"9","bestRating":"12"},
"reviewBody":"Kettcar bleiben die Chronisten der Gegenwart: kluge Texte, große Refrains.",
"itemReviewed":{"@type":"MusicAlbum","name":"Gute Laune ungerecht verteilt","byArtist":{"@type":"MusicGroup","name":"Kettcar"},"datePublished":"2024-03-15"}}]</script>
</head><body><article><p>Kettcar sind zurück, und wie. Eine lange Rezension mit vielen Sätzen über das Album und die Band.</p></article></body></html>`

const hotelSeite = `<html><head><title>Hotel Seeblick</title><script type="application/ld+json">{"@graph":[{"@type":"Hotel","name":"Hotel Seeblick",
"address":{"addressLocality":"Ahlbeck","addressCountry":"DE"},"offers":{"@type":"Offer","price":"129","priceCurrency":"EUR"},
"description":"Direkt an der Ostsee."}]}</script></head><body><main><p>Willkommen im Hotel Seeblick, direkt an der Promenade von Ahlbeck auf Usedom.</p></main></body></html>`

const artikelSeite = `<html><head><title>Warum Brot altbacken wird</title><meta name="description" content="Stärke kristallisiert."></head>
<body><header>Kopf Kopf Kopf Kopf Kopf Kopf Kopf Kopf Kopf Kopf</header><article><p>Brot wird nicht trocken, weil es Wasser verliert, sondern weil die Stärke wieder kristallisiert.</p>
<p>Im Kühlschrank geht das schneller als bei Zimmertemperatur, eingefroren fast gar nicht.</p></article><script>var x = "kein Text";</script></body></html>`

// a site with the pages above, and a page that is not there
func site(t *testing.T) *httptest.Server {
	t.Helper()
	pages := map[string]string{"/rezept": rezeptSeite, "/album": albumSeite, "/hotel": hotelSeite, "/artikel": artikelSeite}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/weiter" {
			http.Redirect(w, r, "/artikel", http.StatusFound)
			return
		}
		p, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(p))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func noteDir(t *testing.T) (*notes.Dir, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := notes.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d, dir
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// haus#117: a recipe page becomes a recipe note — portions, time,
// ingredients, steps, the link — from the page's own JSON-LD, with no
// model; the same link again is not kept twice.
func TestMerkenEinRezept(t *testing.T) {
	srv := site(t)
	d, dir := noteDir(t)
	f := Fetcher{AllowPrivate: true}
	got, err := Merken(context.Background(), f, nil, d, "Schau mal: "+srv.URL+"/rezept #weihnachten für die Feiertage", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Art != "rezept" || got.Titel != "Lasagne al forno" || got.Kurz != "Rezept gemerkt: Lasagne al forno, 3 Zutaten, 2 Schritte." {
		t.Fatalf("%+v", got)
	}
	n := read(t, dir, got.Name)
	for _, want := range []string{"art: rezept", "tags: [kochen]", "quelle: " + srv.URL + "/rezept", "# Lasagne al forno",
		"*Für 4 Portionen · 1 Std. 30 Min.*", "## Zutaten\n\n- 500 g Hackfleisch\n- 1 Pck. Lasagneplatten\n- 200 g Käse, gerieben",
		"## Zubereitung\n\n1. Hackfleisch anbraten.\n2. Schichten & backen.", "Quelle: " + srv.URL + "/rezept"} {
		if !strings.Contains(n, want) {
			t.Errorf("lacks %q:\n%s", want, n)
		}
	}
	if strings.Contains(n, "Menü") || strings.Contains(n, "langer Text") {
		t.Errorf("the page's noise in a recipe:\n%s", n)
	}
	again, err := Merken(context.Background(), f, nil, d, srv.URL+"/rezept", "")
	if err != nil || !again.Schon || again.Name != got.Name {
		t.Fatalf("twice: %+v %v", again, err)
	}
}

// A review of an album: band, album, year, the rating with its site, the
// review's core; tag musik. The hint's #words are tags, its other words
// a line.
func TestMerkenEinAlbum(t *testing.T) {
	srv := site(t)
	d, dir := noteDir(t)
	got, err := Merken(context.Background(), Fetcher{AllowPrivate: true}, nil, d, srv.URL+"/album", "#hören unbedingt live")
	if err != nil {
		t.Fatal(err)
	}
	n := read(t, dir, got.Name)
	for _, want := range []string{"art: album", "tags: [hören, musik]", "# Kettcar – Gute Laune ungerecht verteilt", "*2024 · Wertung 9/12 (VISIONS.de)*",
		"Kettcar bleiben die Chronisten der Gegenwart", "Dazu: unbedingt live", "## Von der Seite"} {
		if !strings.Contains(n, want) {
			t.Errorf("lacks %q:\n%s", want, n)
		}
	}
	if got.Kurz != "Album gemerkt: Kettcar – Gute Laune ungerecht verteilt, Wertung 9/12." {
		t.Errorf("%q", got.Kurz)
	}
}

// A hotel: place, provider, price, its description; tag reisen. A plain
// article: the house model's core and tags, its text kept; a redirect
// followed, the link kept as sent.
func TestMerkenReiseUndArtikel(t *testing.T) {
	srv := site(t)
	d, dir := noteDir(t)
	got, err := Merken(context.Background(), Fetcher{AllowPrivate: true}, nil, d, srv.URL+"/hotel", "")
	if err != nil {
		t.Fatal(err)
	}
	n := read(t, dir, got.Name)
	for _, want := range []string{"art: reise", "tags: [reisen]", "# Hotel Seeblick", "- Ort: Ahlbeck, DE", "- Preis: 129 EUR", "Direkt an der Ostsee."} {
		if !strings.Contains(n, want) {
			t.Errorf("lacks %q:\n%s", want, n)
		}
	}

	p := proxyfake.New("gemma-4-e4b")
	defer p.Close()
	p.Content = `{"art":"notiz","kern":"Altes Brot ist nicht trocken: Die Stärke kristallisiert neu. Kühlen beschleunigt das, Einfrieren hält es auf.","tags":["Brot","Küche"]}`
	model := &judge.Client{URL: p.Base(), Model: "gemma-4-e4b", Key: "k"}
	got, err = Merken(context.Background(), Fetcher{AllowPrivate: true}, model, d, srv.URL+"/weiter", "")
	if err != nil {
		t.Fatal(err)
	}
	n = read(t, dir, got.Name)
	for _, want := range []string{"tags: [brot, küche]", "quelle: " + srv.URL + "/weiter", "# Warum Brot altbacken wird", "Altes Brot ist nicht trocken",
		"## Von der Seite\n\nBrot wird nicht trocken", "eingefroren fast gar nicht"} {
		if !strings.Contains(n, want) {
			t.Errorf("lacks %q:\n%s", want, n)
		}
	}
	if strings.Contains(n, "art:") || strings.Contains(n, "kein Text") || strings.Contains(n, "Kopf Kopf") {
		t.Errorf("a plain note, without the page's script and header:\n%s", n)
	}
	if !strings.HasPrefix(got.Kurz, "Notiz gemerkt: Altes Brot ist nicht trocken") {
		t.Errorf("%q", got.Kurz)
	}
}

// What cannot be had: not an address; an address at home (never fetched,
// also behind a name); a page that is not there is kept as link and title,
// and said so.
func TestMerkenWhatCannotBeHad(t *testing.T) {
	srv := site(t)
	d, dir := noteDir(t)
	ctx := context.Background()
	if _, err := Merken(ctx, Fetcher{}, nil, d, "keine adresse hier", ""); !errors.Is(err, ErrNoAddress) {
		t.Errorf("no address: %v", err)
	}
	for _, home := range []string{srv.URL + "/rezept", "http://192.168.178.1/", "http://localhost:8650/", "http://fritz.box/", "http://[::1]/", "http://100.64.1.2/"} {
		if _, err := Merken(ctx, Fetcher{Wait: 2 * time.Second}, nil, d, home, ""); !errors.Is(err, ErrPrivate) {
			t.Errorf("%s: %v, want ErrPrivate", home, err)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("a refused link made a note: %v", entries)
	}
	if _, err := Merken(ctx, Fetcher{AllowPrivate: true, Wait: time.Second}, nil, d, "http://127.0.0.1:1/nichts", ""); !errors.Is(err, ErrNoAnswer) {
		t.Errorf("no answer: %v", err)
	}
	got, err := Merken(ctx, Fetcher{AllowPrivate: true}, nil, d, srv.URL+"/fehlt", "")
	if err != nil || !got.Teilweise || !strings.Contains(got.Kurz, "nur Link und Titel") {
		t.Fatalf("%+v %v", got, err)
	}
	if n := read(t, dir, got.Name); !strings.Contains(n, "Die Seite ließ sich nicht lesen") || !strings.Contains(n, "quelle: "+srv.URL+"/fehlt") {
		t.Errorf("%s", n)
	}
	if u, _ := url.Parse("http://192.168.0.1"); u != nil {
		if _, _, err := (Fetcher{}).Fetch(ctx, u); !errors.Is(err, ErrPrivate) {
			t.Errorf("Fetch home: %v", err)
		}
	}
}

func TestDuration(t *testing.T) {
	for in, want := range map[string]string{"PT1H30M": "1 Std. 30 Min.", "PT45M": "45 Min.", "P1DT2H": "26 Std.", "": "", "x": ""} {
		if got := duration(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
