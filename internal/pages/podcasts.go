package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/abs"
	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/podcasts"
)

// PodcastsPath is the family's podcasts (haus#123): subscribe by a link,
// one's own subscriptions with Abo beenden, their newest episodes.
const PodcastsPath = "/podcasts"

const (
	saysPodcastsHow  = "Einen Podcast abonniert die Box für dich: Link von Apple Podcasts, von der Website des Podcasts oder die Feed-Adresse. Sie lädt immer die letzten 3 Folgen; bei jeder neuen bekommst du auf Signal Bescheid."
	saysPodcastsHear = "Anhören in der Audiobookshelf-App oder in Symfonium mit Audiobookshelf."
	saysPodcastsDown = "Audiobookshelf antwortet gerade nicht."
)

// Podcasts is the page and its two POSTs: tun=abonnieren (link), and
// tun=beenden (feed), each for the person signed in.
func Podcasts(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.Podcasts == nil || r.URL.Path != PodcastsPath {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		person := r.Header.Get("Remote-User")
		if r.Method == http.MethodPost {
			if !fromHaus(r) {
				http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
				return
			}
			r.ParseForm()
			ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
			defer cancel()
			var said string
			var err error
			switch r.PostForm.Get("tun") {
			case "abonnieren":
				res, e := cfg.Podcasts.Abonnieren(ctx, person, r.PostForm.Get("link"))
				said, err = res.Satz, e
			case "beenden":
				said, err = cfg.Podcasts.Beenden(ctx, person, r.PostForm.Get("feed"))
			default:
				http.Error(w, "Das geht hier nicht.", http.StatusBadRequest)
				return
			}
			if err != nil {
				if !knownPodcastError(err) {
					log.Printf("haus: podcasts: %s: %v", person, err)
					err = errors.New(saysPodcastsDown)
				}
				writePodcasts(w, r, cfg, http.StatusBadRequest, "", err.Error(), r.PostForm.Get("link"))
				return
			}
			log.Printf("haus: podcasts: %s %s", person, r.PostForm.Get("tun"))
			writePodcasts(w, r, cfg, http.StatusOK, said, "", "") // said on the page, never through its address
			return
		}
		if !showOnly(w, r, "Podcasts") {
			return
		}
		writePodcasts(w, r, cfg, http.StatusOK, "", "", "")
	}
}

// knownPodcastError is a refusal said in the family's words already.
func knownPodcastError(err error) bool { return podcasts.Bekannt(err) }

// writePodcasts is the page: what was just said, the subscribe form, one's
// podcasts and their newest episodes.
func writePodcasts(w http.ResponseWriter, r *http.Request, cfg StartConfig, code int, done, problem, link string) {
	person := r.Header.Get("Remote-User")
	b := familyHead(r, cfg, "Podcasts", "Podcasts")
	b.WriteString(noteStyle + "<h1>Podcasts</h1><p class=k>" + html.EscapeString(saysPodcastsHow) + "</p>")
	if done != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(done) + "</p></div>")
	}
	if problem != "" {
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(problem) + "</p></div>")
	}
	b.WriteString(`<section class=karte aria-labelledby=k-abo><h2 id=k-abo>Podcast abonnieren</h2><form class=notizform method=post action=` + PodcastsPath + `>` +
		`<input type=hidden name=tun value=abonnieren><label for=link>Link zum Podcast</label><input id=link type=url name=link required inputmode=url autocomplete=off placeholder="https://podcasts.apple.com/…" value="` + html.EscapeString(link) + `">` +
		`<button data-laeuft="Sucht den Feed …">Abonnieren</button></form><p class=k>` + html.EscapeString(saysPodcastsHear) + `</p></section>`)
	mine, err := cfg.Podcasts.Meine(person)
	if err != nil {
		log.Printf("haus: podcasts: %v", err)
	}
	c := cards.Card{Title: "Meine Podcasts"}
	var b2 strings.Builder
	for _, a := range mine {
		b2.WriteString(`<li class=tu><div>` + html.EscapeString(a.Title) + `</div><form method=post action=` + PodcastsPath + ` data-frage="„` + html.EscapeString(a.Title) + `“ nicht mehr abonnieren?">` +
			`<input type=hidden name=tun value=beenden><input type=hidden name=feed value="` + html.EscapeString(a.Feed) + `"><button data-laeuft="Beendet …">Abo beenden</button></form></li>`)
	}
	if len(mine) == 0 {
		c.Add("Noch kein Podcast abonniert", "")
		b.WriteString(c.HTML())
	} else {
		b.WriteString(`<section class=karte aria-labelledby=k-meine><h2 id=k-meine>Meine Podcasts</h2><ul>` + b2.String() + `</ul></section>`)
	}
	if len(mine) > 0 {
		var eps []abs.Episode
		var eerr error
		fetch(r, func(ctx context.Context) {
			lib, err := cfg.Podcasts.ABS.PodcastLibrary(ctx)
			if err != nil {
				eerr = err
				return
			}
			eps, eerr = cfg.Podcasts.ABS.Recent(ctx, lib, 50)
		})
		mineItem := map[string]bool{}
		for _, a := range mine {
			mineItem[a.Item] = true
		}
		f := cards.Card{Title: "Neueste Folgen"}
		if eerr != nil {
			log.Printf("haus: podcasts: recent: %v", eerr)
			f.Add(saysPodcastsDown, "")
		}
		now, loc := cfg.clock()
		n := 0
		for _, e := range eps {
			if !mineItem[e.ItemID] || n == 10 {
				continue
			}
			n++
			f.Add(e.Title, e.Podcast+" · "+dayWord(e.Published, now, loc))
		}
		if eerr == nil && n == 0 {
			f.Add("Noch keine Folge geladen", "Die Box holt sie in den nächsten Minuten.")
		}
		b.WriteString(f.HTML())
	}
	writePage(w, code, b)
}
