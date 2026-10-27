package pages

import (
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/mdopp/haus/internal/code128"
)

// TrennblattPath is the separator sheets' print page (haus#161).
const TrennblattPath = ScannenPath + "/trennblatt"

// What a separator sheet carries (haus#161): PATCH T, which Laib's scan
// service (laib#814) and Paperless-ngx split a stack at — exactly these
// seven characters — and what it is for.
const (
	separatorCode    = "PATCH T"
	saysSeparator    = "Trennblatt – zwischen zwei Dokumente legen. Wiederverwendbar."
	saysSeparatorHow = "Ein Trennblatt kommt zwischen zwei Dokumente; dort trennt die Box den Stapel."
	saysSeparatorMax = "Höchstens 50 auf einmal."
	separatorsPlain  = 5
	separatorsMax    = 50
)

// separatorSVG is the barcode, drawn once: 1.2 mm a module, 112 modules
// (13.4 cm) and 10 white on either side, 3 cm tall — at 300 dpi a module
// is 14 pixels, plenty for a gray scan.
var separatorSVG = func() string {
	w, err := code128.B(separatorCode)
	if err != nil {
		panic(err)
	}
	return code128.SVG(w, 1.2, 30, 10)
}()

// separators is Scannen's part for the sheets: what they do, how many,
// and the button to the print page.
func separators() string {
	// owner 2026-10-27: rarely needed, so folded away
	return `<details class=trennblaetter><summary>Trennblätter drucken</summary><p class=k>` + html.EscapeString(saysSeparatorHow) + `</p>` +
		`<form method=get action=` + TrennblattPath + ` class=trennblatt><label for=anzahl>Wie viele?</label>` +
		`<input id=anzahl type=number name=anzahl value=` + strconv.Itoa(separatorsPlain) + ` min=1 max=` + strconv.Itoa(separatorsMax) + ` step=1 required inputmode=numeric>` +
		`<button data-laeuft="Öffnet …">Trennblätter drucken</button></form></details>`
}

// separatorCount is ?anzahl= within 1–50; none or no number is 5, and
// whether more were asked for than the page holds.
func separatorCount(s string) (int, bool) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	switch {
	case errors.Is(err, strconv.ErrRange):
		if strings.HasPrefix(s, "-") {
			return 1, false
		}
		return separatorsMax, true
	case err != nil:
		return separatorsPlain, false
	case n < 1:
		return 1, false
	case n > separatorsMax:
		return separatorsMax, true
	}
	return n, false
}

// Trennblatt is the print page: as many separator sheets as asked for,
// one A4 page each, the barcode inline; Drucken and Zurück above them,
// not printed. Nothing comes from elsewhere.
func Trennblatt(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Trennblätter gibt es nur zum Ansehen und Drucken.", http.StatusMethodNotAllowed)
			return
		}
		n, capped := separatorCount(r.URL.Query().Get("anzahl"))
		cfg.Usage.Count("scannen.trennblatt") // what was opened, never by whom (haus#50)
		what := strconv.Itoa(n) + " Trennblätter"
		if n == 1 {
			what = "1 Trennblatt"
		}
		var b strings.Builder
		b.WriteString("<!doctype html><html lang=de><meta charset=utf-8><meta name=viewport content='width=device-width,initial-scale=1'>" +
			"<meta name=color-scheme content='dark light'><title>Trennblätter · Zuhause</title>" + frame + separatorStyle)
		b.WriteString(`<div class=leiste><a href=` + DokumentePath + `#scanner>Zurück</a><p><b>` + what + `</b>`)
		if capped {
			b.WriteString(` <span class=k>` + html.EscapeString(saysSeparatorMax) + `</span>`)
		}
		b.WriteString(`</p><button type=button onclick="print()">Drucken</button></div><main class=blaetter>`)
		sheet := `<section class=blatt>` + separatorSVG + `<p class=code>` + separatorCode + `</p><p class=wozu>` + html.EscapeString(saysSeparator) + `</p></section>`
		for range n {
			b.WriteString(sheet)
		}
		b.WriteString(`</main>`)
		writePage(w, http.StatusOK, &b)
	}
}

// separatorStyle: on screen the sheets as white pages under a bar; in
// print only the sheets, one per A4 page, black on white.
const separatorStyle = `<style>body{margin:0;background:var(--bg);color:var(--ink);font-family:Geist,system-ui,sans-serif}
.leiste{position:sticky;top:0;display:flex;flex-wrap:wrap;align-items:center;gap:12px;padding:12px 16px;background:var(--panel);border-bottom:1px solid var(--line)}
.leiste p{flex:1;margin:0}.leiste .k{color:var(--muted)}.leiste a{color:var(--accent);min-height:44px;display:flex;align-items:center}
.leiste button{min-height:44px;padding:0 20px;border:0;border-radius:10px;background:var(--accent);color:#fff;font:inherit;font-weight:600}
.blaetter{padding:16px}
.blatt{box-sizing:border-box;width:210mm;height:296mm;overflow:hidden;margin:0 auto;padding:60mm 0 0;background:#fff;color:#000;text-align:center;page-break-after:always;break-after:page}
.blatt:last-child{page-break-after:auto;break-after:auto}
.blatt svg{display:block;margin:0 auto}.blatt .code{margin:6mm 0 0;font:600 8mm/1 Geist,system-ui,sans-serif;letter-spacing:.1em}.blatt .wozu{margin:20mm 15mm 0;font:400 6mm/1.3 Geist,system-ui,sans-serif}
@media screen{.blatt{max-width:100%;height:auto;aspect-ratio:210/297;padding-top:20%;margin-bottom:16px;border-radius:4px}.blatt svg{max-width:90%;height:auto}}
@media print{.leiste{display:none}body{background:#fff}.blaetter{padding:0}}
@page{size:A4;margin:0}</style>`
