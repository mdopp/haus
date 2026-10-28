package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/ansicht"
	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/laib"
)

// Deine Kalender (haus#66): every calendar lives in Radicale (laib#495), and
// here the family adds and keeps them. Three ways in: a Google calendar by
// its secret iCal address (Haus hands it to laib once and never shows it
// again), a public calendar (Feiertage, Schulferien, any ICS address), a new
// calendar for the family or for oneself. Per calendar: a colour and
// whether Kalender shows it (Haus's own view, internal/ansicht), Trennen or
// Löschen, and its address for the phone.
const (
	kalenderVerwalten = "/kalender/verwalten"
	kalenderAdd       = "/kalender/hinzufuegen"
	kalenderChange    = "/kalender/aendern"

	saysGoogleHow = "So findest du die Adresse: Google Kalender im Browser öffnen, oben rechts ⚙ → Einstellungen, links unter „Einstellungen für meine Kalender“ den Kalender wählen, dann „Kalender integrieren“ → „Geheime Adresse im iCal-Format“ kopieren. Haus gibt sie an die Box weiter und zeigt sie nie wieder."
	saysPhoneHow  = "Auf dem Telefon: DAVx⁵ (Android) oder Einstellungen → Kalender → Accounts (iPhone) → Konto hinzufügen → CalDAV, diese Adresse, Anmeldung wie bei Haus."
	saysBookHow   = "Genauso für die Kontakte: in DAVx⁵ dasselbe Konto, beim iPhone ein CardDAV-Account mit dieser Adresse."
	saysNoLaibCal = "Kalender verbinden geht auf dieser Box noch nicht: Laib kann es hier noch nicht (laib calendar, laib#495)."
)

// what the page says after a change: a fixed sentence per outcome, so the
// address never lands in a URL
var kalenderSaid = map[string]string{
	"verbunden":      "Verbunden. Die Termine erscheinen in den nächsten 15 Minuten.",
	"abstand":        "Gespeichert. Der Kalender wird ab jetzt so oft abgeglichen.",
	"getrennt":       "Getrennt. Beim nächsten Abgleich verschwindet die Kopie.",
	"angelegt":       "Der Kalender ist angelegt.",
	"geloescht":      "Der Kalender ist gelöscht.",
	"gespeichert":    "Gespeichert.",
	"keine-adresse":  "Das ist keine Kalender-Adresse. Sie beginnt mit https:// oder webcal://.",
	"keine-google":   "Das ist nicht Googles „Geheime Adresse im iCal-Format“. Sie beginnt mit https://calendar.google.com/ und endet auf .ics.",
	"kein-name":      "Bitte einen Namen eintragen.",
	"gibt-es-schon":  "Einen Kalender mit diesem Namen gibt es schon.",
	"nicht-moeglich": "Das ging gerade nicht. Später noch einmal versuchen; geht es dann wieder nicht, Bescheid sagen, wer die Box betreut.",
	"kein-laib":      saysNoLaibCal,
}

// everyChoices is "Wie oft abgleichen?" (laib#619, haus#96): laib's
// interval and the words for it; a Google calendar suggests every run,
// a public one monthly.
var everyChoices = []struct{ Every, Words string }{
	{"15m", "alle 15 Minuten"}, {"1d", "täglich"}, {"7d", "wöchentlich"}, {"30d", "monatlich"},
}

// everySelect is the choice, the given one selected.
func everySelect(selected string) string {
	var b strings.Builder
	b.WriteString("<select name=abstand>")
	for _, c := range everyChoices {
		sel := ""
		if c.Every == selected {
			sel = " selected"
		}
		b.WriteString(`<option value="` + c.Every + `"` + sel + ">" + c.Words + "</option>")
	}
	b.WriteString("</select>")
	return b.String()
}

// everyOf is a subscription's interval, its kind's default when laib has
// none (Google every run, a public calendar monthly).
func everyOf(a laib.Abo) string {
	if a.Every != "" {
		return a.Every
	}
	if a.Kind == "google" {
		return "15m"
	}
	return "30d"
}

// everyWords is an interval in the family's words.
func everyWords(every string) string {
	for _, c := range everyChoices {
		if c.Every == every {
			return c.Words
		}
	}
	return "alle " + every
}

// validEvery is one of the choices.
func validEvery(every string) bool {
	for _, c := range everyChoices {
		if c.Every == every {
			return true
		}
	}
	return false
}

// publicCalendars are the suggestions under "Öffentlichen Kalender
// abonnieren": OpenHolidays (openholidaysapi.org) answers ICS per Land;
// laib's kalender adds the years.
var bundeslaender = []struct{ Code, Name string }{
	{"BW", "Baden-Württemberg"}, {"BY", "Bayern"}, {"BE", "Berlin"}, {"BB", "Brandenburg"}, {"HB", "Bremen"}, {"HH", "Hamburg"},
	{"HE", "Hessen"}, {"MV", "Mecklenburg-Vorpommern"}, {"NI", "Niedersachsen"}, {"NW", "Nordrhein-Westfalen"}, {"RP", "Rheinland-Pfalz"},
	{"SL", "Saarland"}, {"SN", "Sachsen"}, {"ST", "Sachsen-Anhalt"}, {"SH", "Schleswig-Holstein"}, {"TH", "Thüringen"},
}

func holidaysURL(kind, land string) string {
	return "https://openholidaysapi.org/" + kind + "?countryIsoCode=DE&subdivisionCode=DE-" + land + "&languageIsoCode=DE"
}

// calSlug is a calendar's short name from what the person typed.
func calSlug(name string) string {
	name = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss").Replace(strings.ToLower(strings.TrimSpace(name)))
	var b strings.Builder
	dash := false
	for _, c := range name {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(c)
			dash = false
		} else {
			dash = true
		}
		if b.Len() >= 36 {
			break
		}
	}
	return b.String()
}

// KalenderVerwalten is the page.
func KalenderVerwalten(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Deine Kalender") {
			return
		}
		if cfg.Calendar == nil {
			notSetCalendar.write(w, r, cfg, "Kalender")
			return
		}
		me := r.Header.Get("Remote-User")
		var (
			cals    []caldav.Calendar
			calErr  error
			abos    []laib.Abo
			aboErr  error
			view, _ = cfg.Views.For(me)
		)
		fetch(r,
			func(ctx context.Context) { cals, calErr = cfg.Calendar.PersonCalendars(ctx, me) },
			func(ctx context.Context) {
				if cfg.Laib != nil {
					abos, aboErr = cfg.Laib.CalendarList(ctx)
				}
			})
		logErrs("kalender verwalten", map[string]error{"calendars": calErr, "laib calendar": aboErr})
		now, loc := cfg.clock()
		b := familyHead(r, cfg, "Deine Kalender", "Kalender")
		b.WriteString(kalenderStyle + "<h1>Deine Kalender</h1><p><a href=/kalender>Zurück zum Kalender</a></p>")
		if said := kalenderSaid[r.URL.Query().Get("gesagt")]; said != "" {
			b.WriteString("<p class=gesagt role=status>" + html.EscapeString(said) + "</p>")
		}
		if calErr != nil {
			b.WriteString("<p class=k>" + saysNoCalendar + "</p>")
		}
		b.WriteString("<section class=karte aria-labelledby=k-liste><h2 id=k-liste>Kalender</h2><ul class=kalender>")
		seen := map[string]bool{}
		for _, k := range cals {
			seen[k.Path] = true
			b.WriteString(calendarRow(cfg, k, view, aboFor(abos, k.Path), now, loc))
		}
		for _, a := range abos { // connected, not mirrored yet
			p := aboPath(a)
			if !seen[p] && (a.Person == me || a.Person == cfg.Calendar.User) {
				b.WriteString("<li><div><strong>" + html.EscapeString(cmpOr(a.Title, a.Name)) + "</strong> <span class=k>verbunden " + sinceWords(a.Since, now, loc) + ", wird gerade zum ersten Mal abgeglichen</span></div>" +
					changeForm(p, "", "<button name=tu value=trennen data-laeuft=\"Trennt …\">Trennen</button>") + "</li>")
			}
		}
		b.WriteString("</ul></section>")
		radicale := App{"Radicale", cfg.RadicalePublicURL}
		b.WriteString(radicale.Weiter("/.web/", "alle Kalender und Adressbücher, Anmeldung wie bei Haus"))
		if radicale.Set() {
			b.WriteString("<details class=adresse><summary>Adressbuch fürs Telefon</summary>" + copyField(radicale.href("/"+cfg.Calendar.User+"/haushalt-kontakte/")) + "<p class=k>" + saysBookHow + "</p></details>")
		}
		b.WriteString(addCard(cfg.Laib != nil && !errors.Is(aboErr, laib.ErrNoTool)))
		writePage(w, http.StatusOK, b)
	}
}

// aboPath is where laib's kalender mirrors a subscription (laib#495).
func aboPath(a laib.Abo) string {
	if a.Kind == "google" {
		return "/" + a.Person + "/google-" + a.Name + "/"
	}
	return "/" + a.Person + "/abo-" + a.Name + "/"
}

// aboFor is the subscription a mirror at path belongs to, if any.
func aboFor(abos []laib.Abo, path string) *laib.Abo {
	for i, a := range abos {
		if aboPath(a) == path {
			return &abos[i]
		}
	}
	return nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// sinceWords is "seit 21.10." for an RFC 3339 time, "" for none.
func sinceWords(at string, now time.Time, loc *time.Location) string {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return ""
	}
	return "seit " + dayWord(t, now, loc)
}

// agoWords is how long ago: vor 5 min, vor 3 h, vor 2 Tagen.
func agoWords(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 2*time.Minute:
		return "gerade eben"
	case d < 2*time.Hour:
		return "vor " + strconv.Itoa(int(d.Minutes())) + " min"
	case d < 48*time.Hour:
		return "vor " + strconv.Itoa(int(d.Hours())) + " h"
	}
	return "vor " + strconv.Itoa(int(d.Hours()/24)) + " Tagen"
}

// calendarRow is one calendar: its name and colour, where it comes from,
// and what the person may do with it.
func calendarRow(cfg StartConfig, k caldav.Calendar, view ansicht.One, abo *laib.Abo, now time.Time, loc *time.Location) string {
	var b strings.Builder
	b.WriteString("<li><div>")
	if c := view.Color(k.Path); c != "" {
		b.WriteString(`<span class=farbe style="background:` + c + `" aria-hidden=true></span>`)
	}
	b.WriteString("<strong>" + html.EscapeString(k.Name) + "</strong> <span class=k>")
	switch k.Kind {
	case "haushalt":
		b.WriteString("der Haushalt, für alle")
	case "familie":
		b.WriteString("für die Familie")
	case "privat":
		b.WriteString("nur für dich")
	case "google", "abo":
		from := "aus Google"
		if k.Kind == "abo" {
			from = "abonniert"
		}
		if k.Family {
			from += ", für die Familie"
		}
		b.WriteString(from + ", nur lesen")
		if abo != nil {
			b.WriteString(" · verbunden " + sinceWords(abo.Since, now, loc) + " · abgeglichen " + everyWords(everyOf(*abo)))
		}
		if !k.Synced.IsZero() {
			b.WriteString(" · zuletzt abgeglichen " + agoWords(k.Synced, now))
		}
	default:
		b.WriteString("für die Familie")
	}
	if !view.Shows(k.Path) {
		b.WriteString(" · ausgeblendet")
	}
	b.WriteString("</span>")
	if k.Problem != "" {
		b.WriteString("<p class=problem>" + html.EscapeString(k.Problem) + "</p>")
	}
	b.WriteString("</div>")
	var actions strings.Builder
	actions.WriteString("<label>Farbe <select name=farbe>")
	actions.WriteString(`<option value="-">keine</option>`)
	for _, c := range ansicht.Colors {
		sel := ""
		if view.Color(k.Path) == c.Hex {
			sel = " selected"
		}
		actions.WriteString(`<option value="` + c.Hex + `"` + sel + ">" + c.Name + "</option>")
	}
	actions.WriteString("</select></label><button name=tu value=farbe data-laeuft=\"Setzt …\">Farbe setzen</button>")
	if view.Shows(k.Path) {
		actions.WriteString("<button name=tu value=aus data-laeuft=\"Blendet aus …\">Ausblenden</button>")
	} else {
		actions.WriteString("<button name=tu value=ein data-laeuft=\"Blendet ein …\">Einblenden</button>")
	}
	switch {
	case k.ReadOnly() && abo != nil:
		actions.WriteString("<label>Abgleichen " + everySelect(everyOf(*abo)) + "</label><button name=tu value=abstand data-laeuft=\"Speichert …\">Abstand ändern</button>")
		actions.WriteString("<details><summary>Trennen…</summary><button name=tu value=trennen data-laeuft=\"Trennt …\">Ja, „" + html.EscapeString(k.Name) + "“ trennen</button></details>")
	case k.Kind == "familie" || k.Kind == "privat":
		actions.WriteString("<details><summary>Löschen…</summary><button name=tu value=loeschen data-laeuft=\"Löscht …\">Ja, „" + html.EscapeString(k.Name) + "“ mit allen Terminen löschen</button></details>")
	}
	b.WriteString(changeForm(k.Path, k.Name, actions.String()))
	if app := (App{"Radicale", cfg.RadicalePublicURL}); app.Set() {
		b.WriteString("<details class=adresse><summary>Adresse fürs Telefon</summary>" + copyField(app.href(k.Path)) + "<p class=k>" + saysPhoneHow + "</p></details>")
	}
	b.WriteString("</li>")
	return b.String()
}

func changeForm(path, name, inner string) string {
	return `<form method=post action=` + kalenderChange + ` class=knoepfe><input type=hidden name=kalender value="` + html.EscapeString(path) + `"><input type=hidden name=name value="` + html.EscapeString(name) + `">` + inner + "</form>"
}

func copyField(addr string) string {
	return `<input class=kopieren readonly value="` + html.EscapeString(addr) + `" aria-label="Adresse zum Kopieren"><button type=button data-kopieren>Kopieren</button>`
}

// addCard is "Kalender hinzufügen", three ways; without laib's tool only the new calendar.
func addCard(canSubscribe bool) string {
	var b strings.Builder
	b.WriteString("<section class=karte aria-labelledby=k-neu><h2 id=k-neu>Kalender hinzufügen</h2>")
	if canSubscribe {
		b.WriteString("<details><summary>Google-Kalender verbinden</summary><form method=post action=" + kalenderAdd + " class=eingabe><input type=hidden name=weg value=google>" +
			"<label>Name <input name=name maxlength=60 placeholder=Familie required></label>" +
			"<label>Geheime Adresse im iCal-Format <input name=adresse type=url autocomplete=off spellcheck=false required placeholder='https://calendar.google.com/calendar/ical/…/basic.ics'></label>" +
			"<p class=k>" + html.EscapeString(saysGoogleHow) + "</p><label>Wie oft abgleichen? " + everySelect("15m") + "</label><button data-laeuft=\"Verbindet …\">Verbinden</button></form></details>")
		b.WriteString("<details><summary>Öffentlichen Kalender abonnieren</summary><form method=post action=" + kalenderAdd + " class=eingabe><input type=hidden name=weg value=abo>" +
			"<label>Welcher <select name=vorlage><option value=feiertage>Feiertage</option><option value=schulferien>Schulferien</option><option value=eigene>eigene Adresse (z. B. Müllabfuhr der Gemeinde)</option></select></label><label>Bundesland <select name=land>")
		for _, l := range bundeslaender {
			b.WriteString(`<option value="` + l.Code + `">` + l.Name + "</option>")
		}
		b.WriteString("</select></label><label>Eigene Adresse <input name=adresse type=url autocomplete=off spellcheck=false placeholder='https://… oder webcal://…'></label>" +
			"<label>Name <input name=name maxlength=60 placeholder='leer: Feiertage bzw. Schulferien'></label>" + forWhom() +
			"<label>Wie oft abgleichen? " + everySelect("30d") + "</label><button data-laeuft=\"Abonniert …\">Abonnieren</button></form></details>")
	} else {
		b.WriteString("<p class=k>" + saysNoLaibCal + "</p>")
	}
	b.WriteString("<details><summary>Neuen Kalender anlegen</summary><form method=post action=" + kalenderAdd + " class=eingabe><input type=hidden name=weg value=neu>" +
		"<label>Name <input name=name maxlength=60 required></label>" + forWhom() + "<button data-laeuft=\"Legt an …\">Anlegen</button></form></details></section>")
	return b.String()
}

func forWhom() string {
	return "<fieldset><legend>Für wen</legend><label class=wahl><input type=radio name=fuer value=familie checked> für die Familie</label><label class=wahl><input type=radio name=fuer value=ich> nur für mich</label></fieldset>"
}

// kalenderForm is a POST from Haus's own page, parsed; "" and false when
// it is not one (answered already).
func kalenderForm(w http.ResponseWriter, r *http.Request, cfg StartConfig) (string, bool) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, kalenderVerwalten, http.StatusSeeOther)
		return "", false
	}
	if !fromHaus(r) {
		http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
		return "", false
	}
	if cfg.Calendar == nil {
		http.Error(w, notSetCalendar.say, http.StatusNotFound)
		return "", false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Das Formular ist nicht lesbar.", http.StatusBadRequest)
		return "", false
	}
	return r.Header.Get("Remote-User"), true
}

func said(w http.ResponseWriter, r *http.Request, key string) {
	http.Redirect(w, r, kalenderVerwalten+"?gesagt="+url.QueryEscape(key), http.StatusSeeOther)
}

// laibSaid is laib's refusal as a key of kalenderSaid; the address is in none.
func laibSaid(err error) string {
	switch {
	case errors.Is(err, laib.ErrNoTool):
		return "kein-laib"
	case strings.Contains(err.Error(), "Geheime Adresse"):
		return "keine-google"
	case strings.Contains(err.Error(), "keine Kalender-Adresse"):
		return "keine-adresse"
	}
	return "nicht-moeglich"
}

// KalenderHinzufuegen takes the three forms of "Kalender hinzufügen".
func KalenderHinzufuegen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, ok := kalenderForm(w, r, cfg)
		if !ok {
			return
		}
		f := r.PostForm
		title, owner := oneLine(f.Get("name")), me
		if f.Get("fuer") != "ich" && f.Get("weg") != "google" {
			owner = cfg.Calendar.User // the family's: Haus's account's home
		}
		switch f.Get("weg") {
		case "google", "abo":
			if cfg.Laib == nil {
				said(w, r, "kein-laib")
				return
			}
			address, kind := strings.TrimSpace(f.Get("adresse")), "google"
			if f.Get("weg") == "abo" {
				kind = "abo"
				land := f.Get("land")
				if !validLand(land) {
					land = "NI"
				}
				switch f.Get("vorlage") {
				case "feiertage":
					address, title = holidaysURL("PublicHolidays", land), cmpOr(title, "Feiertage "+landName(land))
				case "schulferien":
					address, title = holidaysURL("SchoolHolidays", land), cmpOr(title, "Schulferien "+landName(land))
				}
			}
			slug := calSlug(title)
			switch {
			case slug == "" && kind == "google":
				slug, title = "google", "Google"
			case slug == "":
				said(w, r, "kein-name")
				return
			case address == "":
				said(w, r, "keine-adresse")
				return
			}
			every := f.Get("abstand")
			if !validEvery(every) {
				every = "" // laib's default for its kind
			}
			_, err := cfg.Laib.CalendarSubscribe(r.Context(), laib.Abo{Person: owner, Name: slug, Kind: kind, Title: title, Every: every}, address)
			if err != nil {
				log.Printf("haus: kalender: %s verbindet %s %s/%s: %v", me, kind, owner, slug, err) // laib's errors carry no address
				said(w, r, laibSaid(err))
				return
			}
			log.Printf("haus: kalender: %s verbindet %s %s/%s", me, kind, owner, slug)
			said(w, r, "verbunden")
		case "neu":
			slug := calSlug(title)
			if slug == "" {
				said(w, r, "kein-name")
				return
			}
			_, err := cfg.Calendar.MakeCalendar(r.Context(), me, slug, title, owner != me)
			switch {
			case errors.Is(err, caldav.ErrCalendarExists):
				said(w, r, "gibt-es-schon")
			case err != nil:
				log.Printf("haus: kalender: %s legt %s an: %v", me, slug, err)
				said(w, r, "nicht-moeglich")
			default:
				said(w, r, "angelegt")
			}
		default:
			http.Error(w, "Unbekannter Weg.", http.StatusBadRequest)
		}
	}
}

func validLand(code string) bool { return landName(code) != "" }

func landName(code string) string {
	for _, l := range bundeslaender {
		if l.Code == code {
			return l.Name
		}
	}
	return ""
}

// KalenderAendern is one calendar's buttons: colour, show or hide (the
// person's own view), Trennen (a subscription of theirs or the family's),
// Löschen (a calendar Haus made, the family's or theirs).
func KalenderAendern(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		me, ok := kalenderForm(w, r, cfg)
		if !ok {
			return
		}
		path, family := r.PostForm.Get("kalender"), "/"+cfg.Calendar.User+"/"
		mine := strings.HasPrefix(path, "/"+me+"/") || strings.HasPrefix(path, family)
		if !mine || strings.Count(path, "/") != 3 || !strings.HasSuffix(path, "/") || strings.Contains(path, "..") {
			http.Error(w, "Diesen Kalender änderst du hier nicht.", http.StatusForbidden)
			return
		}
		parts := strings.Split(strings.Trim(path, "/"), "/") // owner, calendar
		owner, name := parts[0], parts[len(parts)-1]
		switch tu := r.PostForm.Get("tu"); tu {
		case "farbe", "aus", "ein":
			hide := tu == "aus"
			var h *bool
			color := ""
			if tu == "farbe" {
				color = r.PostForm.Get("farbe")
			} else {
				h = &hide
			}
			if err := cfg.Views.Set(me, path, h, color); err != nil {
				log.Printf("haus: kalender: %s ansicht %s: %v", me, path, err)
				said(w, r, "nicht-moeglich")
				return
			}
			said(w, r, "gespeichert")
		case "abstand":
			every := r.PostForm.Get("abstand")
			if cfg.Laib == nil || !validEvery(every) || (!strings.HasPrefix(name, "google-") && !strings.HasPrefix(name, "abo-")) {
				http.Error(w, "Das ist kein verbundener Kalender oder kein Abstand.", http.StatusBadRequest)
				return
			}
			if _, err := cfg.Laib.CalendarEvery(r.Context(), owner, name[strings.Index(name, "-")+1:], every); err != nil {
				log.Printf("haus: kalender: %s abstand %s: %v", me, path, err)
				said(w, r, laibSaid(err))
				return
			}
			log.Printf("haus: kalender: %s abstand %s %s", me, path, every)
			said(w, r, "abstand")
		case "trennen":
			if cfg.Laib == nil || (!strings.HasPrefix(name, "google-") && !strings.HasPrefix(name, "abo-")) {
				http.Error(w, "Das ist kein verbundener Kalender.", http.StatusBadRequest)
				return
			}
			_, err := cfg.Laib.CalendarUnsubscribe(r.Context(), owner, name[strings.Index(name, "-")+1:])
			if err != nil {
				log.Printf("haus: kalender: %s trennt %s: %v", me, path, err)
				said(w, r, laibSaid(err))
				return
			}
			log.Printf("haus: kalender: %s trennt %s", me, path)
			said(w, r, "getrennt")
		case "loeschen":
			if err := cfg.Calendar.DeleteCalendar(r.Context(), path); err != nil {
				log.Printf("haus: kalender: %s löscht %s: %v", me, path, err)
				said(w, r, "nicht-moeglich")
				return
			}
			log.Printf("haus: kalender: %s löscht %s", me, path)
			said(w, r, "geloescht")
		default:
			http.Error(w, "Unbekannte Änderung.", http.StatusBadRequest)
		}
	}
}

const kalenderStyle = `<style>.karte form.eingabe{display:block}.kalender select{min-width:9rem;min-height:44px;font:inherit;color:var(--ink);background:var(--card);border:1px solid var(--edge);border-radius:10px;padding:0 10px}.eingabe label{max-width:30rem}.eingabe p.k{max-width:36rem}.kalender{list-style:none;padding:0;margin:0}.kalender li{padding:12px 0;border-bottom:1px solid var(--line)}.kalender .knoepfe{display:flex;flex-wrap:wrap;gap:8px;align-items:center;margin-top:8px}
.kalender details{display:inline-block}.adresse{margin:6px 0}.kopieren{width:100%;max-width:36rem;font:13px ui-monospace,Menlo,Consolas,monospace;padding:.35rem .5rem;border:1px solid var(--line);border-radius:.4rem;background:transparent;color:inherit;box-sizing:border-box}
.eingabe label{display:block;margin:.6rem 0 .15rem}.eingabe input:not([type=radio]),.eingabe select{font:inherit;width:100%;max-width:30rem;padding:.35rem .5rem;border:1px solid var(--line);border-radius:.4rem;background:transparent;color:inherit;box-sizing:border-box}
.eingabe .wahl{display:inline-block;margin-right:1rem}.eingabe fieldset{border:0;padding:0;margin:.6rem 0}.eingabe button{margin-top:.8rem}.problem{border-left:3px solid #b3261e;padding:.1rem .6rem;margin:.4rem 0}.gesagt{border-left:3px solid #2b7a3d;padding:.2rem .7rem}
.farbe{display:inline-block;width:.65em;height:.65em;border-radius:50%;margin-right:.45em}</style>`
