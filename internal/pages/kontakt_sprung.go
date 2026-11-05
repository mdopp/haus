package pages

import (
	"cmp"
	"context"
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

// A contact's two fixed links (owner 2026-11-05): /kontakte/<uid>/dokumente
// and /kontakte/<uid>/nachrichten, keyed by the card's UID, so they hold
// when a name or a number changes. Each reads the card from Radicale and
// sends the viewer on (302): to Paperless's documents of the contact, and
// to the viewer's own message archive searched for it, so each person
// reaches their own. The contact's page shows them as two quiet buttons,
// the phones' address book as two web addresses (KontaktKarte), each only
// where its backend is on the box.

// sprung is one of the links: its place below the contact's page, its
// button on the page, its label in the phone book.
type sprung struct {
	was         string
	knopf, kurz texte.ID
}

const (
	sprungDokumente   = "dokumente"
	sprungNachrichten = "nachrichten"
)

// maxArchivSuche is how many numbers and e-mails the archive's search
// takes: msgvault's limit of alternatives (search.MaxORBranches).
const maxArchivSuche = 8

// spruenge are the links the box has: Dokumente with Paperless's public
// address, Nachrichten with a message archive for anyone.
func (cfg StartConfig) spruenge() []sprung {
	var out []sprung
	if cfg.paperless().Set() {
		out = append(out, sprung{sprungDokumente, texte.KontaktDokumente, texte.KontaktDokumenteKurz})
	}
	if cfg.ArchivURL != "" && len(cfg.Archive) > 0 {
		out = append(out, sprung{sprungNachrichten, texte.KontaktNachrichten, texte.KontaktNachrichtenKurz})
	}
	return out
}

// kontaktSprungPfad splits what follows /kontakte/ into the UID and the
// link asked for; "" for the contact's own page.
func kontaktSprungPfad(rest string) (uid, was string) {
	if i := strings.LastIndexByte(rest, '/'); i >= 0 && (rest[i+1:] == sprungDokumente || rest[i+1:] == sprungNachrichten) {
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
// it opens at home only and the viewer is away.
func kontaktSprung(w http.ResponseWriter, r *http.Request, cfg StartConfig, ct caldav.Contact, name, was string) {
	w.Header().Set("Cache-Control", "no-store") // each viewer's own
	say := func(s string) {
		missing{http.StatusOK, s, name, cards.ContactPage(ct.UID), "Zurück zu " + name}.write(w, r, cfg)
	}
	switch {
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
// its label (X-ABLabel), before its end. Back from a phone for uid, a URL
// that is one of uid's links comes out, on whatever host; the person's
// own URLs stay. So a phone's edit never stores them, nor doubles them.
func KontaktKarte(cfg StartConfig) haushalt.Karte {
	pub := strings.TrimRight(cfg.PublicURL, "/")
	return func(card, uid string) string {
		if uid != "" {
			return ohneSpruenge(card, uid)
		}
		root, err := caldav.Parse(card)
		var cs []caldav.Contact
		if err == nil {
			cs = caldav.Contacts(root)
		}
		lines := strings.SplitAfter(card, "\n")
		for i := len(lines) - 1; i >= 0 && pub != "" && len(cs) == 1 && cs[0].UID != ""; i-- {
			if strings.EqualFold(strings.TrimSpace(lines[i]), "END:VCARD") {
				var add []string
				for _, s := range cfg.spruenge() {
					g := "homes-" + s.was
					add = append(add, g+".URL;TYPE=homes:"+pub+cards.ContactPage(cs[0].UID)+"/"+s.was+"\r\n", g+".X-ABLabel:"+caldav.EscapeText(texte.T(s.kurz))+"\r\n")
				}
				return strings.Join(slices.Concat(lines[:i], add, lines[i:]), "")
			}
		}
		return card
	}
}

// unfoldCard joins a vCard's folded lines (RFC 6350 §3.2).
var unfoldCard = strings.NewReplacer("\r\n ", "", "\r\n\t", "", "\n ", "", "\n\t", "")

// ohneSpruenge is a card a phone sent, unfolded, without uid's links.
func ohneSpruenge(card, uid string) string {
	lines := slices.DeleteFunc(strings.Split(unfoldCard.Replace(card), "\n"), func(l string) bool {
		head, v, _ := strings.Cut(strings.TrimRight(l, "\r"), ":")
		name, _, _ := strings.Cut(head, ";")
		u, err := url.Parse(strings.TrimSpace(v))
		if err != nil || !strings.EqualFold(name[strings.LastIndexByte(name, '.')+1:], "URL") {
			return false
		}
		was, ok := strings.CutPrefix(u.Path, cards.ContactPages+uid+"/")
		return ok && (was == sprungDokumente || was == sprungNachrichten)
	})
	return strings.Join(lines, "\n")
}
