package pages

import (
	"context"
	"html"
	"log"
	"net/http"
	"strings"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/haushalt"
)

// Kontakt is one contact's own page (haus#28), the page a contacts widget
// row opens: the name, the numbers and addresses to tap, the birthday.
func Kontakt(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Der Kontakt") {
			return
		}
		if cfg.Contacts == nil {
			toStart(w, r)
			return
		}
		uid := itemRef(r, cards.ContactPages)
		var all []caldav.Contact
		var err error
		if uid != "" {
			fetch(r, func(ctx context.Context) { all, _, err = cfg.Contacts.Contacts(ctx) })
		}
		if err != nil {
			log.Printf("haus: kontakt page: %q: %v", uid, err)
			notThere(w, r, cfg, http.StatusServiceUnavailable, "Die Kontakte lassen sich gerade nicht lesen.", "Kontakt", "/start", "Start")
			return
		}
		for _, ct := range all {
			if uid == "" || ct.UID != uid {
				continue
			}
			name := trimRunes(oneLine(ct.Name), 120)
			if name == "" {
				name = trimRunes(oneLine(ct.Org), 120)
			}
			if name == "" {
				name = "(ohne Namen)"
			}
			b := familyHead(r, cfg, name, "")
			b.WriteString("<h1>" + html.EscapeString(name) + "</h1>")
			c := cards.Card{Title: "Kontakt"}
			if org := trimRunes(oneLine(ct.Org), 120); org != "" && org != name {
				c.Add(org, "")
			}
			for _, p := range ct.Phones {
				if p = oneLine(p); p != "" {
					c.AddLink(p, "Telefon", "tel:"+strings.ReplaceAll(p, " ", ""))
				}
			}
			for _, m := range ct.Emails {
				if m = oneLine(m); m != "" {
					c.AddLink(m, "E-Mail", "mailto:"+m)
				}
			}
			if ct.Birthday != "" {
				c.Add(haushalt.BirthdayText(ct.Birthday), "Geburtstag")
			}
			b.WriteString(c.HTML())
			writePage(w, http.StatusOK, b)
			return
		}
		notThere(w, r, cfg, http.StatusNotFound, "Diesen Kontakt gibt es nicht.", "Kontakt", "/start", "Start")
	}
}
