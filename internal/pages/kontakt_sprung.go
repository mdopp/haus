package pages

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"html"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/cards"
	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/texte"
)

// A contact's fixed links (owner 2026-11-05): /kontakte/<uid>/dokumente,
// /kontakte/<uid>/nachrichten and /kontakte/<uid>/fotos, keyed by the
// card's UID, so they hold when a name or a number changes. Each reads the
// card from Radicale and sends the viewer on (302): to Paperless's
// documents of the contact, to the viewer's own message archive searched
// for it, so each person reaches their own, and to the Immich person of
// its name. The contact's page shows them as quiet buttons, the phones'
// address book as web addresses (KontaktKarte), each only where its
// backend is on the box. A card without a picture gets its Immich face
// there too, made when served and never stored, as the links.

// sprung is one of the links: its place below the contact's page, its
// button on the page, its label in the phone book.
type sprung struct {
	was         string
	knopf, kurz texte.ID
}

const (
	sprungDokumente   = "dokumente"
	sprungNachrichten = "nachrichten"
	sprungFotos       = "fotos"
)

// istSprung: is was one of the links?
func istSprung(was string) bool {
	return was == sprungDokumente || was == sprungNachrichten || was == sprungFotos
}

// fotosAn: Immich is on the box, and Homes knows its address.
func (cfg StartConfig) fotosAn() bool { return cfg.Gesichter != nil && alleAn(cfg, []string{"fotos"}) }

// maxArchivSuche is how many numbers and e-mails the archive's search
// takes: msgvault's limit of alternatives (search.MaxORBranches).
const maxArchivSuche = 8

// spruenge are the links the box has: Dokumente with Paperless's public
// address, Nachrichten with a message archive for anyone, Fotos with Immich.
func (cfg StartConfig) spruenge() []sprung {
	var out []sprung
	if cfg.paperless().Set() {
		out = append(out, sprung{sprungDokumente, texte.KontaktDokumente, texte.KontaktDokumenteKurz})
	}
	if cfg.ArchivURL != "" && len(cfg.Archive) > 0 {
		out = append(out, sprung{sprungNachrichten, texte.KontaktNachrichten, texte.KontaktNachrichtenKurz})
	}
	if cfg.fotosAn() {
		out = append(out, sprung{sprungFotos, texte.KontaktFotos, texte.KontaktFotosKurz})
	}
	return out
}

// kontaktSprungPfad splits what follows /kontakte/ into the UID and the
// link asked for; "" for the contact's own page.
func kontaktSprungPfad(rest string) (uid, was string) {
	if i := strings.LastIndexByte(rest, '/'); i >= 0 && istSprung(rest[i+1:]) {
		return rest[:i], rest[i+1:]
	}
	return rest, ""
}

// kontaktSpruenge are the page's buttons, one per link the box has.
func kontaktSpruenge(cfg StartConfig, uid string) string {
	var b strings.Builder
	for _, s := range cfg.spruenge() {
		b.WriteString(`<p class=weiter-in><a href="` + html.EscapeString(cards.ContactPage(uid)+"/"+s.was) + `">` + html.EscapeString(texte.T(s.knopf)) + "</a></p>")
	}
	return b.String()
}

// kontaktSprung sends the viewer on to was for ct, or says in one line
// why not: the backend is not on the box, the viewer has no archive, or
// it opens at home only and the viewer is away. Immich is public.
func kontaktSprung(w http.ResponseWriter, r *http.Request, cfg StartConfig, ct caldav.Contact, name, was string) {
	w.Header().Set("Cache-Control", "no-store") // each viewer's own
	say := func(s string) {
		missing{http.StatusOK, s, name, cards.ContactPage(ct.UID), "Zurück zu " + name}.write(w, r, cfg)
	}
	switch {
	case was == sprungFotos && !cfg.fotosAn():
		say(texte.T(texte.KontaktKeinImmich))
	case was == sprungFotos:
		http.Redirect(w, r, cfg.Gesichter.Link(cmp.Or(oneLine(ct.Name), oneLine(ct.Org))), http.StatusFound)
	case was == sprungDokumente && !cfg.paperless().Set():
		say(texte.T(texte.KontaktKeinPaperless))
	case was == sprungDokumente && !cfg.paperlessOpens(r):
		say(saysPaperlessLAN)
	case was == sprungDokumente:
		http.Redirect(w, r, cfg.paperless().href(dokumenteVon(r, cfg, ct)), http.StatusFound)
	case cfg.ArchivURL == "" || !cfg.Archive[r.Header.Get("Remote-User")]:
		say(texte.T(texte.KontaktKeinArchiv))
	case !atHome(r): // the archive is the house's network's only (laib's archiv template)
		say(texte.T(texte.KontaktArchivDaheim))
	default:
		http.Redirect(w, r, cfg.ArchivURL+"/?"+archivSuche(cfg, ct), http.StatusFound)
	}
}

// dokumenteVon is Paperless's document list for the contact: filtered by
// the correspondent of its name or else its organisation (the API, with
// the token), otherwise Paperless's full-text search for the name as one phrase. The
// list's address as Paperless's own frontend reads it: correspondent__id__in
// and query (src-ui/src/app/data/filter-rule-type.ts).
func dokumenteVon(r *http.Request, cfg StartConfig, ct caldav.Contact) string {
	names := []string{oneLine(ct.Name), oneLine(ct.Org)}
	for _, n := range names {
		if n == "" || cfg.Docs == nil {
			continue
		}
		id, err := 0, error(nil)
		fetch(r, func(ctx context.Context) { id, err = cfg.Docs.ID(ctx, "correspondents", n) })
		if err != nil {
			log.Printf("homes: kontakt %q: Paperless's correspondents: %v", ct.UID, err)
			break // the search still finds the name
		}
		if id > 0 {
			return "/documents?correspondent__id__in=" + strconv.Itoa(id)
		}
	}
	if q := cmp.Or(names...); q != "" {
		return "/documents?" + url.Values{"query": {`"` + strings.ReplaceAll(q, `"`, "") + `"`}}.Encode() // a phrase: Paperless ORs loose words
	}
	return "/documents"
}

// archivSuche is msgvault's „Everything“ searched for the contact, as its
// web UI keeps a view in its address (workspace, mode, explore with the
// query; web/src/lib/explore/state.svelte.ts): its numbers as +<country>…
// and its e-mails, each quoted, as alternatives (" | "); without either,
// its name.
func archivSuche(cfg StartConfig, ct caldav.Contact) string {
	var terms []string
	add := func(t string) {
		if t = strings.ReplaceAll(strings.TrimSpace(t), `"`, ""); t != "" && !slices.Contains(terms, `"`+t+`"`) && len(terms) < maxArchivSuche {
			terms = append(terms, `"`+t+`"`)
		}
	}
	for _, p := range ct.Phones {
		if d := haushalt.International(p, cmp.Or(cfg.PhoneCountry, haushalt.DefaultCountry)); len(d) > 4 {
			add("+" + d)
		}
	}
	for _, m := range ct.Emails {
		if strings.Contains(m, "@") {
			add(m)
		}
	}
	if len(terms) == 0 {
		add(cmp.Or(oneLine(ct.Name), oneLine(ct.Org)))
	}
	explore, _ := json.Marshal(map[string]any{"schemaVersion": 2, "query": strings.Join(terms, " | ")})
	return url.Values{"workspace": {"everything"}, "mode": {"full_text"}, "explore": {string(explore)}}.Encode()
}

// KontaktKarte is the phones' cards with the contact's links
// (haushalt.Karte). To a phone, a card gets the links the box has as web
// addresses on Homes (PublicURL), each a URL in a group of its own with
// its label (X-ABLabel), and, without a PHOTO of its own, its Immich
// face (gesichtZeile), before its end. Back from a phone for uid, a URL
// that is one of uid's links comes out, on whatever host, and so does
// the face Homes made; the person's own URLs and picture stay. So a
// phone's edit never stores them, nor doubles them.
func KontaktKarte(cfg StartConfig) haushalt.Karte {
	pub := strings.TrimRight(cfg.PublicURL, "/")
	return func(card, uid string) string {
		root, err := caldav.Parse(card)
		var cs []caldav.Contact
		if err == nil {
			cs = caldav.Contacts(root)
		}
		if uid != "" {
			var gemacht []byte // the face Homes would serve for it: a phone may send it back unmarked
			if len(cs) == 1 {
				gemacht = cfg.Gesichter.Bild(cs[0].Name)
			}
			return ohneSpruenge(card, uid, gemacht)
		}
		lines := strings.SplitAfter(card, "\n")
		for i := len(lines) - 1; i >= 0 && pub != "" && len(cs) == 1 && cs[0].UID != ""; i-- {
			if strings.EqualFold(strings.TrimSpace(lines[i]), "END:VCARD") {
				var add []string
				for _, s := range cfg.spruenge() {
					g := "homes-" + s.was
					add = append(add, g+".URL;TYPE=homes:"+pub+cards.ContactPage(cs[0].UID)+"/"+s.was+"\r\n", g+".X-ABLabel:"+caldav.EscapeText(texte.T(s.kurz))+"\r\n")
				}
				if jpg := cfg.Gesichter.Bild(cs[0].Name); cfg.fotosAn() && jpg != nil && !slices.ContainsFunc(lines, istFoto) {
					add = append(add, gesichtZeile(jpg, slices.ContainsFunc(lines, func(l string) bool { return strings.EqualFold(strings.TrimSpace(l), "VERSION:4.0") })))
				}
				return strings.Join(slices.Concat(lines[:i], add, lines[i:]), "")
			}
		}
		return card
	}
}

// unfoldCard joins a vCard's folded lines (RFC 6350 §3.2).
var unfoldCard = strings.NewReplacer("\r\n ", "", "\r\n\t", "", "\n ", "", "\n\t", "")

// The face Homes adds (owner 2026-11-05) is marked twice: it is in a
// homes- group, as the links are, and carries X-HOMES=immich. A phone that
// keeps either sends it back recognisably; one that keeps neither sends
// back the very bytes (gemacht). A picture the person set is none of these.
const gesichtGruppe, gesichtMarke = "homes-foto", "X-HOMES=immich"

// gesichtZeile is the face as one PHOTO, folded: vCard 3.0's base64, or
// 4.0's data URI.
func gesichtZeile(jpg []byte, v4 bool) string {
	b64 := base64.StdEncoding.EncodeToString(jpg)
	l := gesichtGruppe + ".PHOTO;ENCODING=b;TYPE=JPEG;" + gesichtMarke + ":" + b64
	if v4 {
		l = gesichtGruppe + ".PHOTO;MEDIATYPE=image/jpeg;" + gesichtMarke + ":data:image/jpeg;base64," + b64
	}
	var b strings.Builder
	for n := 75; len(l) > n; n = 74 { // ASCII: an octet a character, a fold's space counted (RFC 6350 §3.2)
		b.WriteString(l[:n] + "\r\n ")
		l = l[n:]
	}
	return b.String() + l + "\r\n"
}

// zeile is a content line's group, name (upper case), parameters and value.
func zeile(l string) (gruppe, name, params, wert string) {
	head, wert, _ := strings.Cut(strings.TrimRight(l, "\r\n"), ":")
	name, params, _ = strings.Cut(head, ";")
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		gruppe, name = name[:i], name[i+1:]
	}
	return gruppe, strings.ToUpper(strings.TrimSpace(name)), params, wert
}

func istFoto(l string) bool { _, n, _, _ := zeile(l); return n == "PHOTO" }

// ohneSpruenge is a card a phone sent, unfolded, without uid's links and
// without the face Homes made (gemacht, its bytes; nil: none).
func ohneSpruenge(card, uid string, gemacht []byte) string {
	lines := slices.DeleteFunc(strings.Split(unfoldCard.Replace(card), "\n"), func(l string) bool {
		gruppe, name, params, v := zeile(l)
		if name == "PHOTO" {
			_, b64, data := strings.Cut(v, "base64,")
			if !data {
				b64 = v
			}
			jpg, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
			return strings.HasPrefix(strings.ToLower(gruppe), "homes-") || strings.Contains(strings.ToUpper(params), gesichtMarke[:7]) ||
				(err == nil && gemacht != nil && bytes.Equal(jpg, gemacht))
		}
		u, err := url.Parse(strings.TrimSpace(v))
		if err != nil || name != "URL" {
			return false
		}
		was, ok := strings.CutPrefix(u.Path, cards.ContactPages+uid+"/")
		return ok && istSprung(was)
	})
	return strings.Join(lines, "\n")
}
