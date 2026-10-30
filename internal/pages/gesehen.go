package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/gesehen"
	"github.com/mdopp/haus/internal/jellyfin"
)

// GesehenPath is Gesehen & Gehört (haus#152): what the person signed in
// watched in Jellyfin and listened to in Audiobookshelf, last first, and
// „Was als Nächstes?“ — a title onto the film and book wish list, unless
// the box has it already or they have seen it.
const GesehenPath = "/gesehen"

const (
	saysGesehenHow     = "Was du zuletzt in Jellyfin geschaut und in Audiobookshelf gehört hast. Die Box liest das selbst; eintragen musst du nichts."
	saysNaechstesHow   = "Ein Film, eine Serie oder ein Buch, das du als Nächstes willst: Es kommt auf die Wunschliste „" + gesehen.ListName + "“ – außer die Box hat es schon oder du hast es schon gesehen."
	saysJellyfinDown   = "Jellyfin antwortet gerade nicht. Lade die Seite gleich noch einmal."
	saysJellyfinNoUser = "In Jellyfin hast du noch kein Konto. Melde dich einmal in Jellyfin an; danach steht hier, was du schaust."
	saysJellyfinNone   = "Noch nichts zu Ende geschaut."
	saysABSDown        = "Audiobookshelf antwortet gerade nicht. Lade die Seite gleich noch einmal."
	saysABSNoUser      = "In Audiobookshelf hast du noch kein Konto. Melde dich einmal in Audiobookshelf an; danach steht hier, was du hörst."
	saysABSNone        = "Noch kein Hörbuch angefangen."
	saysWunschDown     = "Die Wunschliste ist gerade nicht erreichbar. Versuch es gleich noch einmal; dein Titel steht noch im Feld."
	saysGesehenWho     = "Diese Seite zeigt, was du geschaut und gehört hast; dafür musst du angemeldet sein."
)

// Gesehen is the page and its one POST, tun=wuenschen (titel, art), for
// the person signed in.
func Gesehen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.Gesehen == nil || r.URL.Path != GesehenPath {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		if r.Method == http.MethodPost {
			if !fromHaus(r) {
				http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
				return
			}
			r.ParseForm()
			if r.PostForm.Get("tun") != "wuenschen" {
				http.Error(w, "Das geht hier nicht.", http.StatusBadRequest)
				return
			}
			person, titel := r.Header.Get("Remote-User"), r.PostForm.Get("titel")
			art := gesehen.ArtVon(r.PostForm.Get("art"))
			ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
			defer cancel()
			res, err := cfg.Gesehen.Wuenschen(ctx, person, titel, art)
			if err != nil {
				if !errors.Is(err, gesehen.ErrNoTitle) {
					log.Printf("haus: gesehen: wish %s: %v", person, err)
					err = errors.New(saysWunschDown)
				}
				writeGesehen(w, r, cfg, http.StatusBadRequest, gesehenForm{problem: err.Error(), titel: titel, art: art})
				return
			}
			log.Printf("haus: gesehen: %s wished a %s (on the list: %v)", person, art, res.Dazu)
			writeGesehen(w, r, cfg, http.StatusOK, gesehenForm{done: res.Satz, art: art})
			return
		}
		if !showOnly(w, r, "Gesehen & Gehört") {
			return
		}
		writeGesehen(w, r, cfg, http.StatusOK, gesehenForm{art: gesehen.Film})
	}
}

// gesehenForm is what the page says after a wish: done, or the problem and
// the title kept.
type gesehenForm struct {
	done, problem, titel string
	art                  gesehen.Art
}

func writeGesehen(w http.ResponseWriter, r *http.Request, cfg StartConfig, code int, f gesehenForm) {
	person := r.Header.Get("Remote-User")
	svc := cfg.Gesehen
	b := familyHead(r, cfg, "Gesehen & Gehört", "Gesehen & Gehört")
	b.WriteString(noteStyle + "<h1>Gesehen &amp; Gehört</h1><p class=k>" + html.EscapeString(saysGesehenHow) + "</p>")
	if f.done != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(f.done) + "</p></div>")
	}
	if f.problem != "" {
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(f.problem) + "</p></div>")
	}
	if person == "" {
		b.WriteString("<p class=k>" + saysGesehenWho + "</p>")
		writePage(w, code, b)
		return
	}
	var (
		filme          []jellyfin.Gesehen
		buecher        []abs.Gehoert
		wuensche       []caldav.Item
		fErr, bErr, wE error
	)
	var reads []func(context.Context)
	if svc.Filme != nil {
		reads = append(reads, func(ctx context.Context) { filme, fErr = svc.Filme.Geschaut(ctx, person, gesehen.Zeigen) })
	}
	if svc.Buecher != nil {
		reads = append(reads, func(ctx context.Context) { buecher, bErr = svc.Buecher.Gehoerte(ctx, person, gesehen.Zeigen) })
	}
	if svc.DAV != nil {
		reads = append(reads, func(ctx context.Context) { wuensche, wE = svc.Wuensche(ctx) })
	}
	fetch(r, reads...)
	now, loc := cfg.clock()
	if svc.Filme != nil {
		c := cards.Card{Title: "Geschaut"}
		switch {
		case errors.Is(fErr, jellyfin.ErrNoUser):
			c.Add(saysJellyfinNoUser, "")
		case fErr != nil:
			log.Printf("haus: gesehen: jellyfin: %v", fErr)
			c.Add(saysJellyfinDown, "")
		case len(filme) == 0:
			c.Add(saysJellyfinNone, "")
		}
		for _, g := range filme {
			c.Add(g.Name, filmDetail(g, now, loc))
		}
		b.WriteString(c.HTML())
	}
	if svc.Buecher != nil {
		c := cards.Card{Title: "Gehört"}
		switch {
		case errors.Is(bErr, abs.ErrNoUser):
			c.Add(saysABSNoUser, "")
		case bErr != nil:
			log.Printf("haus: gesehen: audiobookshelf: %v", bErr)
			c.Add(saysABSDown, "")
		case len(buecher) == 0:
			c.Add(saysABSNone, "")
		}
		for _, h := range buecher {
			c.Add(h.Titel, buchDetail(h, now, loc))
		}
		b.WriteString(c.HTML())
	}
	if svc.DAV != nil {
		b.WriteString(`<section class=karte aria-labelledby=k-naechstes><h2 id=k-naechstes>Was als Nächstes?</h2><form class=notizform method=post action=` + GesehenPath + `>` +
			`<input type=hidden name=tun value=wuenschen><label for=titel>Titel</label><input id=titel name=titel required maxlength=200 autocomplete=off placeholder="z. B. Der Herr der Ringe" value="` + html.EscapeString(f.titel) + `">` +
			`<fieldset class=tagwahl><legend>Was ist es?</legend>`)
		for _, a := range []gesehen.Art{gesehen.Film, gesehen.Serie, gesehen.Buch} {
			chk := ""
			if a == f.art {
				chk = " checked"
			}
			b.WriteString(`<label class=tag><input type=radio name=art value=` + string(a) + chk + `> ` + string(a) + `</label>`)
		}
		b.WriteString(`</fieldset><button data-laeuft="Schaut nach …">Auf die Wunschliste</button></form><p class=k>` + html.EscapeString(saysNaechstesHow) + `</p></section>`)
		c := cards.Card{Title: "Wunschliste " + gesehen.ListName}
		if wE != nil {
			log.Printf("haus: gesehen: list: %v", wE)
			c.Add(saysWunschDown, "")
		} else if len(wuensche) == 0 {
			c.Add("Noch nichts gewünscht", "")
		}
		for _, it := range wuensche {
			c.Add(it.Summary, it.Herkunft)
		}
		b.WriteString(c.HTML())
	}
	writePage(w, code, b)
}

// filmDetail is a film's or series' quieter line: "Film · 2021 · gestern",
// "Serie · S2E3 · Name · vor 3 Tagen".
func filmDetail(g jellyfin.Gesehen, now time.Time, loc *time.Location) string {
	parts := []string{"Film"}
	if g.Serie {
		parts = []string{"Serie"}
		if g.Folge != "" {
			parts = append(parts, g.Folge)
		}
	} else if g.Jahr > 0 {
		parts = append(parts, strconv.Itoa(g.Jahr))
	}
	if !g.Zuletzt.IsZero() {
		parts = append(parts, dayWord(g.Zuletzt, now, loc))
	}
	return strings.Join(parts, " · ")
}

// buchDetail is a book's: "Hörbuch · zu Ende gehört · gestern",
// "Hörbuch · 40 % · heute".
func buchDetail(h abs.Gehoert, now time.Time, loc *time.Location) string {
	parts := []string{"Hörbuch"}
	if h.Fertig {
		parts = append(parts, "zu Ende gehört")
	} else {
		parts = append(parts, strconv.Itoa(int(h.Fortschritt*100+0.5))+" %")
	}
	if !h.Zuletzt.IsZero() {
		parts = append(parts, dayWord(h.Zuletzt, now, loc))
	}
	return strings.Join(parts, " · ")
}
