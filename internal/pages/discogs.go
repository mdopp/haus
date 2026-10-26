package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/discogs"
)

// Discogs is each person's own connection (owner 2026-10-26: "es muss doch
// ein Schlüssel pro User sein"): "Mit Discogs verbinden" sends them to
// Discogs, which asks whether the household's app may use their account;
// back at DiscogsZurueckPath, Haus keeps their token, for them alone. Their
// collection, their wantlist, Platte erfassen into their shelf.
const (
	DiscogsPath        = "/discogs"
	DiscogsZurueckPath = "/discogs/zurueck"

	saysDiscogsHow     = "Verbinde dein eigenes Discogs-Konto: Platte erfassen trägt dann in deine Sammlung ein, Wünsche kommen auf deine Wantlist, und Haus sagt dir, was du schon auf Vinyl hast. Jede Person verbindet ihr eigenes Konto."
	saysDiscogsEnables = "Damit verbindet jede und jeder im Haus das eigene Discogs-Konto: Haus sieht, welche Platten du schon hast, setzt Wünsche auf deine Wantlist und trägt fotografierte Platten in deine Sammlung ein."
	saysDiscogsFirst   = "Verbinde zuerst dein Discogs-Konto: unter Mehr → Discogs, einmal „Mit Discogs verbinden“."
)

// Discogs is the page, its POSTs (tun=verbinden, tun=trennen) and the way
// back from Discogs.
func Discogs(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := r.Header.Get("Remote-User")
		if user == "" || (r.URL.Path != DiscogsPath && r.URL.Path != DiscogsZurueckPath) {
			missing{http.StatusNotFound, "Diese Seite gibt es nicht.", "Mehr", mehrPath, "Mehr"}.write(w, r, cfg)
			return
		}
		if cfg.Discogs == nil { // the household's app is not set: say so, and an admin gets the way to its keys
			if !showOnly(w, r, "Discogs") {
				return
			}
			b := familyHead(r, cfg, "Discogs", "Discogs")
			b.WriteString(keyHintStyle + "<h1>Discogs</h1><p class=k>" + html.EscapeString(saysDiscogsHow) + "</p>")
			b.WriteString(keyHint(r, cfg, "Discogs", saysDiscogsEnables, publicBase(r, cfg)+DiscogsPath, discogsKeys))
			writePage(w, http.StatusOK, b)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if r.URL.Path == DiscogsZurueckPath {
			q := r.URL.Query()
			if q.Get("denied") != "" {
				writeDiscogs(w, r, cfg, "", "Du hast die Verbindung bei Discogs abgelehnt; nichts ist verbunden.")
				return
			}
			name, err := cfg.Discogs.Fertig(ctx, user, q.Get("oauth_token"), q.Get("oauth_verifier"))
			if err != nil {
				if !errors.Is(err, discogs.ErrFremd) {
					log.Printf("haus: discogs: connecting %s: %v", user, err)
					err = errors.New("Discogs hat die Verbindung nicht bestätigt. Versuch es noch einmal.")
				}
				writeDiscogs(w, r, cfg, "", err.Error())
				return
			}
			log.Printf("haus: discogs: %s connected", user)
			writeDiscogs(w, r, cfg, "Verbunden: Haus nutzt jetzt dein Discogs-Konto „"+name+"“.", "")
			return
		}
		if r.Method != http.MethodPost {
			if showOnly(w, r, "Discogs") {
				writeDiscogs(w, r, cfg, "", "")
			}
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.ParseForm()
		switch r.PostForm.Get("tun") {
		case "verbinden":
			authorize, err := cfg.Discogs.Verbinden(ctx, user, publicBase(r, cfg)+DiscogsZurueckPath)
			if err != nil {
				log.Printf("haus: discogs: starting for %s: %v", user, err)
				writeDiscogs(w, r, cfg, "", "Discogs antwortet gerade nicht, oder der App-Schlüssel der Box stimmt nicht. Später noch einmal.")
				return
			}
			http.Redirect(w, r, authorize, http.StatusSeeOther)
		case "trennen":
			if err := cfg.Discogs.Trennen(user); err != nil {
				log.Printf("haus: discogs: %s: %v", user, err)
				http.Error(w, "Das ließ sich gerade nicht speichern.", http.StatusInternalServerError)
				return
			}
			writeDiscogs(w, r, cfg, "Getrennt: Haus nutzt dein Discogs-Konto nicht mehr. Bei Discogs selbst kannst du der App „Joshua“ den Zugriff unter Einstellungen → Anwendungen ganz entziehen.", "")
		default:
			http.Error(w, "Das geht hier nicht.", http.StatusBadRequest)
		}
	}
}

// publicBase is Haus's own address for Discogs to send a person back to.
func publicBase(r *http.Request, cfg StartConfig) string {
	if p := strings.TrimRight(cfg.PublicURL, "/"); p != "" {
		return p
	}
	return "https://" + r.Host
}

func writeDiscogs(w http.ResponseWriter, r *http.Request, cfg StartConfig, said, problem string) {
	user := r.Header.Get("Remote-User")
	b := familyHead(r, cfg, "Discogs", "Discogs")
	b.WriteString(noteStyle + "<h1>Discogs</h1><p class=k>" + html.EscapeString(saysDiscogsHow) + "</p>")
	if said != "" {
		b.WriteString("<div class=gut role=status><span class=punkt></span><p>" + html.EscapeString(said) + "</p></div>")
	}
	if problem != "" {
		b.WriteString("<div class=achtung role=alert><p>" + html.EscapeString(problem) + "</p></div>")
	}
	if k, ok := cfg.Discogs.Konto(user); ok {
		b.WriteString(`<section class=karte aria-labelledby=k-dc><h2 id=k-dc>Dein Konto</h2><p>Verbunden mit „` + html.EscapeString(k.Name) + `“ seit ` + k.Seit.Format("02.01.2006") + `.</p>` +
			`<p class=wege><a class=knopf href=` + PlattePath + `>Platte erfassen</a></p>` +
			`<form method=post action=` + DiscogsPath + ` data-frage="Haus soll dein Discogs-Konto nicht mehr nutzen?"><input type=hidden name=tun value=trennen><button data-laeuft="Trennt …">Trennen</button></form></section>`)
	} else {
		// data-ganz: the browser goes to Discogs itself, and comes back
		b.WriteString(`<section class=karte aria-labelledby=k-dc><h2 id=k-dc>Nicht verbunden</h2><p class=k>Discogs fragt dich gleich, ob die App „Joshua“ deines Haushalts dein Konto nutzen darf. Danach kommst du hierher zurück.</p>` +
			`<form method=post action=` + DiscogsPath + ` data-ganz><input type=hidden name=tun value=verbinden><button data-laeuft="Öffnet Discogs …">Mit Discogs verbinden</button></form></section>`)
	}
	writePage(w, http.StatusOK, b)
}
