package pages

import (
	"context"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// What the Willkommen page says (haus#56): for someone who has never seen
// the box, which apps there are for them and how to set each up — only
// the apps this box has.
const (
	willkommenPath   = "/willkommen"
	welcomeCookie    = "haus_willkommen"
	saysWelcomeLead  = "Hier steht, was es auf dieser Box für dich gibt und wie du es auf dem Handy und am Computer einrichtest. Du meldest dich überall mit deinem Familienkonto an: deinem Benutzernamen und deinem Passwort."
	saysWelcomeAgain = "Du findest diese Seite jederzeit wieder: auf der Startseite ganz unten, „Apps einrichten“."
	saysAppsUnknown  = "Welche Apps es auf der Box gibt, lässt sich gerade nicht nachsehen. Später noch einmal versuchen."
	saysNoAppsYet    = "Auf dieser Box ist noch keine App für dich eingerichtet."
	saysOnStart      = "Weiter zur Startseite"
)

// a welcome card: the app's name in the family's words, and its steps;
// each step plain text, or the page's own markup for an address
type welcomeApp struct {
	service string // the Laib service that has to be there
	title   string
	steps   func(dom string) []string // markup; dom is the box's domain
}

// address is a server address as the person types it.
func address(host, dom string) string {
	return "<span class=adresse>https://" + html.EscapeString(host+"."+dom) + "</span>"
}

// welcomeApps are the apps the page knows, in the order a new person
// needs them. The hosts are the templates' routes (laib templates/*/laib.yml).
var welcomeApps = []welcomeApp{
	{"immich", "Fotos", func(d string) []string {
		return []string{
			"Lade die App „Immich“ aus dem App Store oder von Google Play.",
			"Als Server-Adresse trägst du ein: " + address("photos", d),
			"Tippe auf „Mit Familienkonto anmelden“ und melde dich mit deinem Benutzernamen und Passwort an.",
			"In der App unter „Sicherung“ schaltest du ein, dass neue Fotos vom Handy von selbst auf die Box kommen.",
		}
	}},
	{"media", "Filme und Musik", func(d string) []string {
		return []string{
			"Lade die App „Jellyfin“ – es gibt sie fürs Handy, für viele Fernseher und für den Computer.",
			"Als Server-Adresse trägst du ein: " + address("media", d),
			"Melde dich mit deinem Benutzernamen und Passwort an.",
		}
	}},
	{"radicale", "Kalender und Kontakte", func(d string) []string {
		return []string{
			"Android: Lade die App „DAVx⁵“, tippe auf „Konto hinzufügen“ und wähle „Mit URL und Benutzername anmelden“.",
			"iPhone: Einstellungen → Kalender → Accounts → Account hinzufügen → Andere → CalDAV-Account.",
			"Als Server-Adresse trägst du ein: " + address("caldav", d) + " – dazu deinen Benutzernamen und dein Passwort.",
			"Danach stehen die Termine der Familie in der Kalender-App deines Handys.",
		}
	}},
	{"vaultwarden", "Passwörter", func(d string) []string {
		return []string{
			"Lade die App „Bitwarden“ aus dem App Store oder von Google Play.",
			"Stell beim Anmelden „Selbst gehostet“ ein und trag als Server-Adresse ein: " + address("vault", d),
			"Gib deine E-Mail-Adresse ein, wähle „Single Sign-On“ und melde dich mit deinem Familienkonto an.",
			"Beim ersten Mal legst du ein Master-Passwort für deinen Tresor fest. Das kennst nur du – niemand kann es zurücksetzen, auch kein Admin. Schreib es dir auf und heb es gut auf.",
		}
	}},
}

// hausAppAPK is the Haus app's newest stable release (mdopp/haus-android).
const hausAppAPK = "https://github.com/mdopp/haus-android/releases/latest/download/app-release.apk"

// hausAppSteps are the Haus app's steps (haus#175): Android installs it,
// an iPhone puts the page on its home screen.
func hausAppSteps(dom string) []string {
	return []string{
		`Android: Lade die Haus-App herunter: <a href="` + hausAppAPK + `">Haus-App laden</a>. Beim ersten Mal fragt das Handy, ob dein Browser Apps installieren darf: Erlauben.`,
		"Öffne die App und trag als Adresse ein: " + address("haus", dom),
		"Melde dich mit deinem Benutzernamen und Passwort an. Die App richtet sich dann selbst ein.",
		"iPhone: Öffne " + address("haus", dom) + " in Safari, tippe auf „Teilen“ und dann auf „Zum Home-Bildschirm“.",
	}
}

// signalSteps are the house's chat on Signal (haus#175), with the persona's name.
func signalSteps(number string) []string {
	n := html.EscapeString(number)
	link := "https://signal.me/#p/" + url.PathEscape(number)
	name := html.EscapeString(House.Display)
	return []string{
		"Lade die App „Signal“, falls du sie noch nicht hast.",
		`Speichere die Nummer <span class=adresse>` + n + `</span> als „` + name + `“ in deinen Kontakten, oder öffne sie direkt: <a href="` + link + `">` + name + ` in Signal öffnen</a>.`,
		"Schreib „Hallo“. " + name + " antwortet und kennt dich, sobald ein Admin dich einmal freigegeben hat.",
	}
}

// Willkommen is the welcome page (haus#56): where a new person lands
// after the first login, and later from Start. It shows, it changes
// nothing; it notes in a cookie that this device has seen it, so Start
// sends there only once.
func Willkommen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Die Willkommensseite") {
			return
		}
		var (
			services map[string]bool
			laibErr  error
		)
		if cfg.Laib != nil {
			fetch(r, func(ctx context.Context) {
				_, rows, err := cfg.Laib.Status(ctx)
				laibErr = err
				services = map[string]bool{}
				for _, s := range rows {
					services[s.Name] = true
				}
			})
		}
		if laibErr != nil {
			log.Printf("haus: willkommen: laib: %v", laibErr)
		}
		http.SetCookie(w, &http.Cookie{Name: welcomeCookie, Value: "gesehen", Path: "/", MaxAge: 5 * 365 * 24 * 3600, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		b := familyHead(r, cfg, "Willkommen", "")
		b.WriteString(netzStyle)
		b.WriteString(willkommenDruck)
		b.WriteString("<style>.karte ol{margin:0;padding:0 0 0 1.4em;background:var(--card);border:1px solid var(--edge);border-radius:14px}.karte ol li{padding:10px 14px 10px 4px;border-top:1px solid var(--line);overflow-wrap:anywhere}.karte ol li:first-child{border-top:0}.weiter{margin:26px 0 0}</style>")
		title := "Willkommen"
		if first := strings.Fields(r.Header.Get("Remote-Name")); len(first) > 0 {
			title += ", " + first[0]
		}
		b.WriteString("<h1>" + html.EscapeString(title) + "</h1><p class=lead>" + html.EscapeString(saysWelcomeLead) + "</p>")
		if person := r.Header.Get("Remote-User"); person != "" {
			b.WriteString("<p>Dein Benutzername: <span class=adresse>" + html.EscapeString(person) + "</span></p>")
		}
		dom := boxDomain(r, cfg)
		shown := 0
		steps := func(title string, ss []string) { // a card of numbered steps
			shown++
			b.WriteString("<section class=karte><h2>" + html.EscapeString(title) + "</h2><ol>")
			for _, s := range ss {
				b.WriteString("<li>" + s + "</li>")
			}
			b.WriteString("</ol></section>")
		}
		if dom != "" { // haus#175: the house's own app first, then its chat
			steps("Die Haus-App", hausAppSteps(dom))
		}
		if cfg.BoteSignal != "" {
			steps(House.Display+" auf Signal", signalSteps(cfg.BoteSignal))
		}
		switch {
		case cfg.Laib == nil:
		case laibErr != nil:
			b.WriteString("<p>" + html.EscapeString(saysAppsUnknown) + "</p>")
		default:
			for _, app := range welcomeApps {
				if !services[app.service] || dom == "" {
					continue
				}
				shown++
				b.WriteString("<section class=karte><h2>" + html.EscapeString(app.title) + "</h2><ol>")
				for _, s := range app.steps(dom) {
					b.WriteString("<li>" + s + "</li>")
				}
				b.WriteString("</ol></section>")
			}
		}
		if cfg.Samba != nil {
			shown++
			c := cardsList("Dateien am Computer")
			c.items = append(c.items, "<li><a href="+netzlaufwerkPath+">Netzlaufwerk einrichten<span class=k>Die Dateien des Hauses im Explorer, im Finder oder in der Dateien-App öffnen. Dafür legst du dort ein eigenes Passwort fest.</span></a></li>")
			b.WriteString(c.html())
		}
		if shown == 0 && laibErr == nil {
			b.WriteString("<p>" + html.EscapeString(saysNoAppsYet) + "</p>")
		}
		b.WriteString(`<p class=drucken><button type=button onclick="print()">Diese Seite drucken</button> <span class=k>Zum Mitgeben: alle Schritte auf einem Blatt.</span></p>`)
		b.WriteString("<p class=k>" + html.EscapeString(saysWelcomeAgain) + "</p>")
		b.WriteString("<p class=weiter><a href=/start>" + html.EscapeString(saysOnStart) + "</a></p>")
		writePage(w, http.StatusOK, b)
	}
}

// boxDomain is the box's domain, the part after haus.: from Haus's own
// public address, else from the host the page was asked on.
func boxDomain(r *http.Request, cfg StartConfig) string {
	host := r.Host
	if u, err := url.Parse(cfg.PublicURL); err == nil && u.Host != "" {
		host = u.Host
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if _, dom, ok := strings.Cut(host, "."); ok && dom != "" {
		return dom
	}
	return ""
}

// firstVisit is Start sending a person to the Willkommen page the first
// time this device opens Haus (haus#56).
func firstVisit(w http.ResponseWriter, r *http.Request, cfg StartConfig) bool {
	if !cfg.Welcome || r.Header.Get("Remote-User") == "" || r.URL.RawQuery != "" {
		return false
	}
	if _, err := r.Cookie(welcomeCookie); err == nil {
		return false
	}
	http.Redirect(w, r, willkommenPath, http.StatusSeeOther)
	return true
}

// toWillkommen is Start's quiet way back to the page, when there is
// something on it.
func toWillkommen(cfg StartConfig) string {
	if cfg.Laib == nil && cfg.Samba == nil {
		return ""
	}
	return "<p class=einrichten style=\"margin:14px 2px 0;font-size:.8rem\"><a href=" + willkommenPath + ">Apps einrichten</a> <span class=k>– so kommen die Apps der Box auf dein Handy und deinen Computer</span></p>"
}

// willkommenDruck is the page on paper (haus#175): no menu, black on white,
// each card whole, a link's address written out.
const willkommenDruck = `<style>@media print{.tabs,.konto,.drucken,.weiter,nav{display:none!important}body{padding:0;max-width:none;display:block;background:#fff;color:#000}
.karte{break-inside:avoid}a{color:#000}a[href^=http]::after{content:" (" attr(href) ")";font-size:.8em;overflow-wrap:anywhere}}</style>`
