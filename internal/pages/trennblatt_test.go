package pages

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/code128"
)

func getTrennblatt(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("Remote-User", "anna")
	rec := httptest.NewRecorder()
	Trennblatt(StartConfig{}).ServeHTTP(rec, req)
	return rec
}

// haus#161: Scannen offers separator sheets — one line on what they do,
// a number field (5, from 1 to 50) and a button that opens the print page.
func TestScannenOffersSeparatorSheets(t *testing.T) {
	body := scanPage(t, &scanLaib{status: map[string]any{"zustand": "bereit", "scanner": true}}, "family")
	mustContain(t, body, "<h3>Trennblätter</h3>", saysSeparatorHow,
		`<form method=get action=/scannen/trennblatt class=trennblatt>`,
		`<input id=anzahl type=number name=anzahl value=5 min=1 max=50 step=1 required inputmode=numeric>`,
		`<button data-laeuft="Öffnet …">Trennblätter drucken</button>`)
	before(t, body, "<h3>Trennblätter</h3>", "<h3>Letzte Stapel</h3>")
	// no separator sheets where Scannen is not there for the family
	mustNotContain(t, scanPage(t, &scanLaib{unset: true}, "family"), "Trennblätter")
}

// haus#161: the print page is as many A4 sheets as asked for, each its own
// page, with the PATCH T barcode inline (large, with its quiet zone), the
// text under it and the German line; a print button and the way back,
// neither printed; nothing loaded from elsewhere.
func TestTrennblattPrintsTheSheets(t *testing.T) {
	rec := getTrennblatt(t, "GET", "/scannen/trennblatt?anzahl=3")
	if rec.Code != 200 {
		t.Fatalf("%d\n%s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	w, err := code128.B("PATCH T")
	if err != nil {
		t.Fatal(err)
	}
	svg := code128.SVG(w, 1.2, 30, 10)
	if n := strings.Count(body, svg); n != 3 {
		t.Errorf("%d barcodes, want 3", n)
	}
	if n := strings.Count(body, `<section class=blatt>`); n != 3 {
		t.Errorf("%d sheets, want 3", n)
	}
	mustContain(t, body, `@page{size:A4;margin:0}`, `page-break-after:always`, `break-after:page`, `.blatt:last-child{page-break-after:auto;break-after:auto}`,
		`<p class=code>PATCH T</p>`, "Trennblatt – zwischen zwei Dokumente legen. Wiederverwendbar.",
		`<button type=button onclick="print()">Drucken</button>`, `<a href=/dokumente#scanner>Zurück</a>`,
		`@media print{.leiste{display:none}`, "3 Trennblätter")
	mustNotContain(t, body, "https://", "src=", `href=http`, `href="http`, "url(http", "<link") // the SVG's xmlns is a name, not a load
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type %q", ct)
	}
}

// haus#161: no number is 5, one sheet is said as one, and a number outside
// 1–50 is the nearest end of it — the page says how many it holds.
func TestTrennblattCount(t *testing.T) {
	for target, want := range map[string]int{
		"/scannen/trennblatt": 5, "/scannen/trennblatt?anzahl=": 5, "/scannen/trennblatt?anzahl=abc": 5,
		"/scannen/trennblatt?anzahl=1": 1, "/scannen/trennblatt?anzahl=50": 50,
		"/scannen/trennblatt?anzahl=0": 1, "/scannen/trennblatt?anzahl=-4": 1, "/scannen/trennblatt?anzahl=51": 50, "/scannen/trennblatt?anzahl=99999999999999999999": 50,
	} {
		body := getTrennblatt(t, "GET", target).Body.String()
		if n := strings.Count(body, `<section class=blatt>`); n != want {
			t.Errorf("%s: %d sheets, want %d", target, n, want)
		}
	}
	mustContain(t, getTrennblatt(t, "GET", "/scannen/trennblatt?anzahl=1").Body.String(), "1 Trennblatt")
	mustContain(t, getTrennblatt(t, "GET", "/scannen/trennblatt?anzahl=51").Body.String(), "50 Trennblätter", "Höchstens 50 auf einmal.")
}

func TestTrennblattOnlyGet(t *testing.T) {
	if rec := getTrennblatt(t, "POST", "/scannen/trennblatt"); rec.Code != 405 {
		t.Errorf("POST: %d", rec.Code)
	}
}
