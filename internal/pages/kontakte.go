package pages

import (
	"cmp"
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/namen"
	"github.com/mdopp/homes/internal/texte"
)

// KontaktePath is the household's address book as a list.
const KontaktePath = "/kontakte"

// kontakteBuchstaben: from this many contacts on, a card per first letter.
const kontakteBuchstaben = 16

// kontaktZeile is one contact in the list: its name, its page, the first
// number or e-mail to tap and what the search finds it by.
type kontaktZeile struct{ name, seite, ziel, zielText, suche string }

// Kontakte is the household's address book (owner 2026-11-05: „Web zeigt,
// Handy bearbeitet“): the cards /napi/gemeinsam/kontakte serves, by name,
// each leading to its page (Kontakt) with its first number or e-mail to
// tap; a search by name, number and e-mail, live and as ?q=. Where to
// change them is one quiet line, as on the calendar; Radicale not
// answering is Kontakt's plain line (haus#33). For the Immich key's owner,
// a quiet line to Meine Kontakte (kontakte_privat.go).
func Kontakte(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Die Kontakte") {
			return
		}
		if cfg.Contacts == nil {
			notSetContacts.write(w, r, cfg, "Kontakte")
			return
		}
		var all []caldav.Contact
		var err error
		fetch(r, func(ctx context.Context) { all, _, err = cfg.Contacts.Contacts(ctx) })
		b := familyHead(r, cfg, "Kontakte", "Kontakte")
		b.WriteString(kontakteStyle + "<h1>Kontakte</h1><p class=k>" + html.EscapeString(texte.T(texte.KontakteBearbeiten)) + "</p>")
		if err != nil {
			log.Printf("homes: kontakte page: %v", err)
			b.WriteString("<p class=k>" + saysNoContacts + "</p>")
			writePage(w, http.StatusOK, b)
			return
		}
		q := trimRunes(oneLine(r.URL.Query().Get("q")), 80)
		field := texte.T(texte.KontakteSuchen)
		b.WriteString(`<form method=get action=` + KontaktePath + ` role=search><input type=search id=kontakte-suche name=q value="` + html.EscapeString(q) +
			`" placeholder="` + field + `" aria-label="` + field + `" autocomplete=off> <button data-laeuft="Sucht …">Suchen</button></form>`)
		zeilen, gesucht := kontaktZeilen(all, cards.ContactPage), namen.Plain(q)
		zeilen = slices.DeleteFunc(zeilen, func(z kontaktZeile) bool { return !strings.Contains(z.suche, gesucht) })
		keiner := `<p class=k id=kontakte-keiner data-vorlage="` + html.EscapeString(texte.T(texte.KontakteKeiner)) + `"`
		switch {
		case len(all) == 0:
			b.WriteString("<p class=k>" + html.EscapeString(texte.T(texte.KontakteLeer)) + "</p>")
		case len(zeilen) == 0:
			b.WriteString(keiner + ">" + html.EscapeString(fmt.Sprintf(texte.T(texte.KontakteKeiner), q)) + "</p>")
		default:
			b.WriteString(keiner + " hidden></p>")
			kontakteListe(b, zeilen, len(all) >= kontakteBuchstaben)
		}
		b.WriteString(meineKontakteLink(r, cfg) + pruefenLink(r, cfg) + kontakteScript)
		writePage(w, http.StatusOK, b)
	}
}

// kontaktZeilen are the contacts as rows, by name as the family reads it,
// each leading to seite(uid).
func kontaktZeilen(all []caldav.Contact, seite func(string) string) []kontaktZeile {
	var out []kontaktZeile
	for _, ct := range all {
		z := kontaktZeile{name: cmp.Or(trimRunes(oneLine(ct.Name), 120), trimRunes(oneLine(ct.Org), 120), "(ohne Namen)")}
		if uid := strings.TrimSpace(ct.UID); uid != "" {
			z.seite = seite(uid) // a card without one has no page of its own
		}
		hay := []string{z.name, ct.Org}
		for _, p := range ct.Phones {
			if p = oneLine(p); p != "" && z.ziel == "" {
				z.ziel, z.zielText = "tel:"+strings.ReplaceAll(p, " ", ""), p
			}
			hay = append(hay, p)
		}
		for _, m := range ct.Emails {
			if m = oneLine(m); m != "" && z.ziel == "" {
				z.ziel, z.zielText = "mailto:"+m, m
			}
			hay = append(hay, m)
		}
		z.suche = namen.Plain(strings.Join(hay, " "))
		out = append(out, z)
	}
	slices.SortStableFunc(out, func(a, b kontaktZeile) int { return cmp.Compare(namen.Plain(a.name), namen.Plain(b.name)) })
	return out
}

// kontakteListe writes the rows as cards: one, or one per first letter.
func kontakteListe(b *strings.Builder, zeilen []kontaktZeile, buchstaben bool) {
	titel := texte.T(texte.KontakteAdressbuch)
	for i, z := range zeilen {
		if buchstaben {
			titel = buchstabe(z.name)
		}
		if i == 0 || buchstaben && titel != buchstabe(zeilen[i-1].name) {
			id := cards.Card{Title: titel}.ID()
			b.WriteString(map[bool]string{true: "</ul></section>"}[i > 0] + "<section class=\"karte kontakte\" aria-labelledby=" + id + "><h2 id=" + id + ">" + html.EscapeString(titel) + "</h2><ul>")
		}
		b.WriteString(`<li data-suche="` + html.EscapeString(z.suche) + `">`)
		if z.seite != "" {
			b.WriteString(`<a href="` + html.EscapeString(z.seite) + `">` + html.EscapeString(z.name) + "</a>")
		} else {
			b.WriteString("<span>" + html.EscapeString(z.name) + "</span>")
		}
		if z.ziel != "" {
			b.WriteString(` <a class=ruf href="` + html.EscapeString(z.ziel) + `">` + html.EscapeString(z.zielText) + "</a>")
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ul></section>")
}

// buchstabe is a name's letter in the list: its first letter without its
// accent, # for anything else.
func buchstabe(name string) string {
	if c := []rune(namen.Plain(name)); len(c) > 0 && unicode.IsLetter(c[0]) && c[0] < utf8.RuneSelf {
		return strings.ToUpper(string(c[0]))
	}
	return "#"
}

// The row's number or e-mail is a tap of its own above the row's link.
const kontakteStyle = `<style>.kontakte li a.ruf{display:inline-block;position:relative;z-index:1;margin-top:2px;padding:4px 0;font-size:.9rem;color:var(--accent)}.kontakte li a.ruf::after{content:none}</style>`

// kontakteScript filters the rows while typing, by the same plain words
// as the server (namen.Plain); a letter's card with no row left hides.
const kontakteScript = `<script>(function(){if(window.homesKontakte)return;window.homesKontakte=1;
var um={'ä':'a','ö':'o','ü':'u','ß':'ss','é':'e','è':'e','á':'a','à':'a','ó':'o','í':'i','&':'und'};
function plain(s){return s.toLowerCase().replace(/[äöüßéèáàóí&]/g,function(c){return um[c]}).replace(/[^\p{L}\p{N}]/gu,'')}
document.addEventListener('input',function(e){if(e.target.id!=='kontakte-suche')return;var q=plain(e.target.value),n=0;
document.querySelectorAll('.kontakte li').forEach(function(li){li.hidden=li.dataset.suche.indexOf(q)<0;if(!li.hidden)n++});
document.querySelectorAll('section.kontakte').forEach(function(s){s.hidden=!s.querySelector('li:not([hidden])')});
var k=document.getElementById('kontakte-keiner');if(k){k.hidden=n>0;k.textContent=n?'':k.dataset.vorlage.replace('%s',e.target.value)}})})()</script>`
