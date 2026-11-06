package pages

import (
	"cmp"
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/paperless"
	"github.com/mdopp/homes/internal/texte"
)

// DokumenteSuchePath is a list of the household's documents in Homes
// (owner 2026-11-06: „ein Einstieg, bei dem ich meine Dokumente –
// Kategorien und Rechnungsnummern/Versicherungsart+Nr/gekaufte Produkte –
// von der Oberfläche suchbar habe“): by words (?q=), Bereich, type,
// person, or one of the shortcuts (?nur=versicherungen|garantie). A row
// opens the document in Paperless. With ?teil=1 only the list: the
// search field fetches it while typing, the page stays.
const DokumenteSuchePath = "/dokumente/suche"

// dokMax is how many rows a list shows.
const dokMax = 50

// dokSuche is what the list was asked for.
type dokSuche struct{ q, bereich, typ, person, nur string }

func dokSucheAus(r *http.Request) dokSuche {
	v := r.URL.Query()
	get := func(k string) string { return trimRunes(oneLine(v.Get(k)), 80) }
	s := dokSuche{get("q"), get("bereich"), get("typ"), get("person"), get("nur")}
	if s.nur != "versicherungen" && s.nur != "garantie" {
		s.nur = ""
	}
	return s
}

func (s dokSuche) leer() bool { return s == dokSuche{} }

// link is the list's address with one more part set.
func (s dokSuche) link(k, v string) string {
	q := url.Values{}
	for _, p := range [][2]string{{"q", s.q}, {"bereich", s.bereich}, {"typ", s.typ}, {"person", s.person}, {"nur", s.nur}, {k, v}} {
		if p[1] != "" {
			q.Set(p[0], p[1])
		} else if p[0] == k {
			q.Del(k)
		}
	}
	return DokumenteSuchePath + "?" + q.Encode()
}

func (s dokSuche) filter(now time.Time) paperless.Filter {
	f := paperless.Filter{Text: s.q, Bereich: s.bereich, Person: s.person, Max: dokMax}
	if s.typ != "" {
		f.Typen = []string{s.typ}
	}
	switch s.nur {
	case "versicherungen": // owner 2026-11-06: Versicherungsschein or Vertrag, in the Bereich Versicherung
		f.Bereich, f.Typen = "Versicherung", paperless.VertragsArten
	case "garantie":
		f.GarantieAb = now
	}
	return f
}

func (s dokSuche) titel() string {
	switch {
	case s.nur == "versicherungen":
		return texte.T("dok.versicherungen")
	case s.nur == "garantie":
		return texte.T("dok.garantie")
	case s.q != "":
		return fmt.Sprintf(texte.T("dok.gesucht"), s.q)
	}
	t := strings.TrimSpace(strings.Join([]string{s.bereich, s.typ}, " · "))
	t = strings.Trim(t, " ·")
	if s.person != "" {
		t = strings.TrimSpace(t + " " + fmt.Sprintf(texte.T("dok.fuer"), s.person))
	}
	return cmp.Or(t, texte.T("dok.alle"))
}

// DokumenteSuche is the list (DokumenteSuchePath).
func DokumenteSuche(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Die Suche") {
			return
		}
		if cfg.Docs == nil {
			notSetDocs.write(w, r, cfg, "Dokumente")
			return
		}
		s := dokSucheAus(r)
		if r.URL.Query().Get("teil") == "1" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			if !s.leer() {
				fmt.Fprint(w, dokTreffer(r, cfg, s))
			}
			return
		}
		b := familyHead(r, cfg, s.titel(), "Dokumente")
		b.WriteString(dokFindenStyle + `<p class=k><a href=` + DokumentePath + `>‹ ` + texte.T("dok.zurueck") + `</a></p><h1>` + html.EscapeString(s.titel()) + "</h1>" +
			dokSuchForm(s) + `<div id=dsuche-treffer data-fehler="` + html.EscapeString("<p class=k>"+texte.T("dok.fehler")+"</p>") + `">` + dokTreffer(r, cfg, s) + "</div>" + dokSucheScript)
		writePage(w, http.StatusOK, b)
	}
}

// dokSuchForm is the one search field; on a list, its Bereich, type and
// person stay set.
func dokSuchForm(s dokSuche) string {
	field, knopf := texte.T("dok.suchen.feld"), texte.T("dok.suchen")
	var keep strings.Builder
	for _, p := range [][2]string{{"bereich", s.bereich}, {"typ", s.typ}, {"person", s.person}} {
		if p[1] != "" {
			keep.WriteString(`<input type=hidden name=` + p[0] + ` value="` + html.EscapeString(p[1]) + `">`)
		}
	}
	return `<form class=dsuche method=get action=` + DokumenteSuchePath + ` role=search><input type=search name=q value="` + html.EscapeString(s.q) + `" placeholder="` + field +
		`" aria-label="` + field + `" autocomplete=off enterkeyhint=search>` + keep.String() + `<button data-laeuft="` + texte.T("dok.sucht") + `" data-knopf="` + knopf + `">` + knopf + `</button></form>`
}

// dokFinden is the Dokumente page's first section: the search field, the
// shortcuts, a tile per Bereich with its count, a chip per type and per
// person, each opening its list in Homes; and, read in the same wait, the
// five documents the archive got last. "" without Paperless's API.
func dokFinden(r *http.Request, cfg StartConfig) (string, cards.Card) {
	if cfg.Docs == nil {
		return "", cards.Card{}
	}
	var o *paperless.Ordnung
	var docs []paperless.Doc
	var err error
	fetch(r, func(ctx context.Context) { o, err = cfg.Docs.Ordnung(ctx) }, func(ctx context.Context) {
		var err error
		if docs, err = cfg.Docs.Recent(ctx, 5); err != nil {
			log.Printf("homes: dokumente: recent: %v", err)
		}
	})
	zuletzt := zuletztAbgelegt(r, cfg, docs)
	b := strings.Builder{}
	b.WriteString(`<section class=finden aria-labelledby=finden><h2 id=finden>` + texte.T("dok.finden") + "</h2>" + dokSuchForm(dokSuche{}) +
		`<div id=dsuche-treffer data-fehler="` + html.EscapeString("<p class=k>"+texte.T("dok.fehler")+"</p>") + `"></div>`)
	if err != nil {
		log.Printf("homes: dokumente: order: %v", err)
		b.WriteString("<p class=k>" + texte.T("dok.fehler") + "</p></section>")
		return b.String(), zuletzt
	}
	chip := func(href, text string, n int) string {
		if n > 0 {
			text += " <span>" + strconv.Itoa(n) + "</span>"
		}
		return `<li><a href="` + html.EscapeString(href) + `">` + text + "</a></li>"
	}
	none := dokSuche{}
	b.WriteString("<ul class=chips>" + chip(none.link("nur", "versicherungen"), texte.T("dok.versicherungen"), 0) + chip(none.link("nur", "garantie"), texte.T("dok.garantie"), 0) + "</ul>")
	if len(o.Bereiche) > 0 {
		b.WriteString("<ul class=bereiche>")
		for _, g := range o.Bereiche {
			b.WriteString(`<li><a href="` + html.EscapeString(none.link("bereich", g.Name)) + `"><b>` + html.EscapeString(g.Name) + "</b><span>" +
				texte.Anzahl(g.Anzahl, "Dokument", "Dokumente") + "</span></a></li>")
		}
		b.WriteString("</ul>")
	}
	b.WriteString("<ul class=chips>")
	for _, g := range o.Typen {
		b.WriteString(chip(none.link("typ", g.Name), html.EscapeString(g.Name), g.Anzahl))
	}
	for _, p := range o.Personen {
		b.WriteString(chip(none.link("person", p), html.EscapeString(fmt.Sprintf(texte.T("dok.fuer"), p)), 0))
	}
	b.WriteString("</ul></section>" + dokSucheScript)
	return b.String(), zuletzt
}

// dokTreffer is the list's rows, each to its document in Paperless (where
// Paperless opens: a LAN-only one away from home is said once, the rows
// stay without a link); under a list cut at dokMax, how to narrow it.
func dokTreffer(r *http.Request, cfg StartConfig, s dokSuche) string {
	now, loc := cfg.clock()
	var es []paperless.Eintrag
	var err error
	fetch(r, func(ctx context.Context) { es, err = cfg.Docs.Finde(ctx, s.filter(now)) })
	if err != nil {
		log.Printf("homes: dokumente: search: %v", err)
		return "<p class=k>" + texte.T("dok.fehler") + "</p>"
	}
	if len(es) == 0 {
		leer := map[string]string{"versicherungen": "dok.versicherungen.leer", "garantie": "dok.garantie.leer"}[s.nur]
		if s.q != "" {
			return "<p class=k>" + html.EscapeString(fmt.Sprintf(texte.T("dok.keine"), s.q)) + "</p>"
		}
		return "<p class=k>" + texte.T(texte.ID(cmp.Or(leer, "dok.leer"))) + "</p>"
	}
	out := ""
	opens := cfg.paperlessOpens(r)
	if cfg.paperless().Set() && !opens {
		out = zustand("gelb", saysPaperlessLAN)
	}
	c := cards.Card{Title: texte.Anzahl(len(es), "Dokument", "Dokumente")}
	for _, e := range es {
		title, detail := dokZeile(e, s.nur, loc)
		c.AddLink(title, detail, map[bool]string{true: cards.DocumentPage(e.ID)}[opens])
	}
	if len(es) == dokMax {
		c.Note = fmt.Sprintf(texte.T("dok.mehr"), dokMax)
	}
	return out + c.HTML()
}

// dokZeile is a row: its title, and the date, sender, type and its key
// field; an insurance by its kind, number and „kündbar bis“; a warranty
// by what was bought and until when.
func dokZeile(e paperless.Eintrag, nur string, loc *time.Location) (string, string) {
	f := e.Felder
	produkt, _, _ := strings.Cut(strings.TrimSpace(f["Produkte"]), "\n")
	nummer := ""
	for _, n := range []string{"Rechnungsnummer", "Versicherungsnummer", "Vertragsnummer", "Kundennummer"} {
		if v := f[n]; v != "" {
			nummer = fmt.Sprintf(texte.T(texte.ID("dok.nr."+n)), v)
			break
		}
	}
	title := cmp.Or(strings.TrimSpace(e.Title), "Dokument "+strconv.Itoa(e.ID))
	var parts []string
	switch nur {
	case "versicherungen":
		title = cmp.Or(produkt, title)
		parts = []string{cmp.Or(f["Anbieter"], e.Correspondent), nummer}
		if !e.KuendigenBis.IsZero() {
			parts = append(parts, fmt.Sprintf(texte.T("dok.kuendbar"), e.KuendigenBis.Format("02.01.2006")))
		} else if t := f["Nächster Kündigungstermin"]; t != "" {
			parts = append(parts, fmt.Sprintf(texte.T("dok.termin"), t))
		}
	case "garantie":
		title = cmp.Or(produkt, title)
		parts = []string{fmt.Sprintf(texte.T("dok.garantie.bis"), f["Garantie bis"]), e.Correspondent}
	default:
		day := ""
		if !e.Created.IsZero() {
			day = e.Created.In(loc).Format("02.01.2006")
		}
		parts = []string{day, e.Correspondent, e.Art, nummer}
		if v := f["Betrag"]; v != "" {
			parts = append(parts, v)
		}
		if v := f["Garantie bis"]; v != "" {
			parts = append(parts, fmt.Sprintf(texte.T("dok.garantie.bis"), v))
		}
	}
	var keep []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			keep = append(keep, p)
		}
	}
	return title, strings.Join(keep, " · ")
}

// A tile per Bereich, two to a row on a phone, four on a wide screen; the
// chips wrap; neither has a border (the Homes look).
const dokFindenStyle = `<style>.finden{margin:18px 0 0}.finden h2{margin:0;font-size:1rem}.dsuche input[type=search]{flex:1;width:auto}
.chips{display:flex;flex-wrap:wrap;gap:8px;margin:14px 0 0;padding:0;list-style:none}.chips a{display:inline-flex;align-items:center;gap:6px;min-height:40px;padding:0 14px;border-radius:999px;background:var(--accent-soft);color:var(--accent);font-weight:600;font-size:.9rem;text-decoration:none}
.chips a span{font-weight:400;color:var(--muted)}.bereiche{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin:14px 0 0;padding:0;list-style:none}
.bereiche a{display:flex;flex-direction:column;justify-content:center;min-height:64px;padding:10px 14px;border-radius:14px;background:var(--card);color:var(--ink);text-decoration:none}
.bereiche b{font-weight:600;overflow-wrap:anywhere}.bereiche span{font-size:.85rem;color:var(--muted)}@media (min-width:40rem){.bereiche{grid-template-columns:repeat(4,minmax(0,1fr))}}
#dsuche-treffer[aria-busy=true]{opacity:.6}</style>`

// dokSucheScript fetches the list into its place while typing (from three
// letters, after a pause) and on Suchen, instead of a new page; on the
// list's own page the address follows. No answer within 15 s: the
// place says so, the button is back.
const dokSucheScript = `<script>(function(){if(window.homesDokSuche)return;window.homesDokSuche=1;var t=null,n=0;
function hol(f){var z=document.getElementById('dsuche-treffer');if(!z||!window.fetch||!window.AbortController)return false;
var u=f.getAttribute('action')+'?'+new URLSearchParams(new FormData(f)).toString(),b=f.querySelector('button'),k=++n,ctl=new AbortController(),uhr=setTimeout(function(){ctl.abort()},15e3);
if(b){b.disabled=true;b.textContent=b.getAttribute('data-laeuft')}z.setAttribute('aria-busy','true');
function fertig(){clearTimeout(uhr);if(k!==n)return;z.removeAttribute('aria-busy');if(b){b.disabled=false;b.textContent=b.getAttribute('data-knopf')}}
fetch(u+'&teil=1',{credentials:'same-origin',headers:{'X-Homes-Skript':'1'},signal:ctl.signal}).then(function(r){if(!r.ok)throw 0;return r.text()})
.then(function(h){if(k!==n)return;z.innerHTML=h;if(location.pathname===f.getAttribute('action'))history.replaceState(null,'',u);fertig()})
.catch(function(){if(k===n)z.innerHTML=z.getAttribute('data-fehler');fertig()});return true}
document.addEventListener('submit',function(e){var f=e.target;if(f.classList&&f.classList.contains('dsuche')&&hol(f))e.preventDefault()},true);
document.addEventListener('input',function(e){var f=e.target.form;if(!f||!f.classList.contains('dsuche'))return;clearTimeout(t);var v=e.target.value.trim();
if(v.length>0&&v.length<3)return;t=setTimeout(function(){hol(f)},400)})})()</script>`
