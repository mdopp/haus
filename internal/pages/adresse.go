package pages

import (
	"context"
	"errors"
	"html"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/mdopp/haus/internal/cards"
	"github.com/mdopp/haus/internal/laib"
)

// Adresse (laib#561) is the household's address, box.yml zuhause, for
// admins: what Laib has, and a form that proposes a change through
// laib_zuhause. Haus decides nothing: Laib files a class-human proposal,
// and the change applies once a person approves it in Laib's inbox. Laib
// never geocodes, and neither does Haus: the coordinates come from this
// device's position (the browser asks) or from OpenStreetMap, which the
// person opens knowingly. The address is in no URL Haus makes and no log
// line; the page is not "Zuhause", which is Haus's own name in every title.
const (
	adressePath = "/adresse"
	adresseSet  = "/adresse/vorschlagen"

	saysAdresseFor    = "Die Box nutzt die Adresse als Standort von Home Assistant, später auch für die Solarprognose und den Müllkalender."
	saysNoLaibZuhause = "Auf dieser Box kann Haus die Adresse noch nicht ändern: Laib kennt sie hier noch nicht, oder policy.yml gibt der Hand haus das Verb zuhause noch nicht (laib#561). Das trägt ein Admin in Laib ein."
	saysNoAdresse     = "Die Adresse lässt sich gerade nicht lesen. Später noch einmal versuchen; sonst weiß laib diag mehr."
)

// what the page says after Vorschlagen: a fixed sentence per outcome, so
// no value lands in the URL
var adresseSaid = map[string]string{
	"vorgeschlagen":  "Vorgeschlagen. Die Änderung gilt, sobald du sie in der Inbox freigibst.",
	"plz":            "Die Postleitzahl nimmt Laib so nicht: 2 bis 10 Zeichen, nur Buchstaben, Ziffern, Leerzeichen und Bindestriche.",
	"land":           "Das Land bitte als zwei Buchstaben, für Deutschland DE.",
	"koordinaten":    "Breite und Länge gehören zusammen: beide eintragen oder beide leer lassen. Die Breite liegt zwischen −90 und 90, die Länge zwischen −180 und 180.",
	"hoehe":          "Die Höhe bitte in ganzen Metern, zwischen −500 und 9000.",
	"zeile":          "Jedes Feld bitte in einer Zeile.",
	"nicht-moeglich": "Das ging gerade nicht. Später noch einmal versuchen; sonst weiß ein Admin in laib diag mehr.",
	"kein-laib":      saysNoLaibZuhause,
}

// zuhauseFields are the form's fields, which are laib_zuhause's.
var zuhauseFields = []string{"street", "postcode", "city", "country", "latitude", "longitude", "elevation"}

// Adresse is the page.
func Adresse(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Adresse") || !adminOnly(w, r) {
			return
		}
		var (
			z   laib.Zuhause
			err = errors.New("no laib")
		)
		if cfg.Laib != nil {
			fetch(r, func(ctx context.Context) { z, err = cfg.Laib.ZuhauseShow(ctx) })
		}
		b := familyHead(r, cfg, "Adresse", "Adresse")
		b.WriteString(adresseStyle + "<h1>Adresse</h1><p class=k>" + saysAdresseFor + "</p>")
		q := r.URL.Query()
		if key := q.Get("gesagt"); adresseSaid[key] != "" && !(key == "kein-laib" && errors.Is(err, laib.ErrNoTool)) {
			class := "gesagt"
			if key != "vorgeschlagen" {
				class = "gesagt nein"
			}
			b.WriteString("<p class='" + class + "' role=status>" + html.EscapeString(adresseSaid[key]))
			if key == "vorgeschlagen" {
				b.WriteString(" " + inboxLink(cfg.LaibURL, q.Get("vorschlag")))
			}
			b.WriteString("</p>")
		}
		switch {
		case errors.Is(err, laib.ErrNoTool):
			b.WriteString("<p>" + html.EscapeString(saysNoLaibZuhause) + "</p>")
		case err != nil:
			log.Printf("haus: adresse page: laib zuhause show: %v", err) // show's errors are Laib's, without the address
			b.WriteString("<p>" + saysNoAdresse + "</p>")
		default:
			b.WriteString(adresseNow(z))
			b.WriteString(adresseForm(z))
		}
		writePage(w, http.StatusOK, b)
	}
}

// inboxLink is the way to Laib's inbox, the one place where a change is
// approved, with the proposal's number when Laib gave one.
func inboxLink(laibURL, nr string) string {
	text := "Zur Inbox"
	if n, err := strconv.Atoi(nr); err == nil && n > 0 {
		text += " (Vorschlag #" + strconv.Itoa(n) + ")"
	}
	if laibURL == "" {
		return ""
	}
	return `<a href="` + html.EscapeString(strings.TrimRight(laibURL, "/")) + `/inbox">` + text + "</a>"
}

func num(f *float64) string {
	if f == nil {
		return ""
	}
	return strconv.FormatFloat(*f, 'f', -1, 64)
}

// adresseNow is the card with what Laib has.
func adresseNow(z laib.Zuhause) string {
	c := cards.Card{Title: "Eingetragen"}
	place := strings.TrimSpace(z.Postcode + " " + z.City)
	if z.Street == "" && place == "" && z.Latitude == nil {
		c.Add("Noch keine Adresse eingetragen", "Home Assistant behält bis dahin seinen eigenen Standort.")
		return c.HTML()
	}
	if z.Street != "" || place != "" {
		lines := strings.TrimSpace(z.Street)
		if place != "" {
			if lines != "" {
				lines += ", "
			}
			lines += place
		}
		c.Add(lines, countryName(z.Country))
	}
	if z.Latitude != nil && z.Longitude != nil {
		detail := "Breite, Länge"
		if z.Elevation != nil {
			detail += " · " + strconv.Itoa(*z.Elevation) + " m über dem Meer"
		}
		c.Add(num(z.Latitude)+", "+num(z.Longitude), detail)
	} else {
		c.Add("Ohne Koordinaten", "Laib schlägt keine Adresse nach: ohne Breite und Länge behält Home Assistant seinen Standort.")
	}
	if z.Elevation != nil && z.Latitude == nil {
		c.Add(strconv.Itoa(*z.Elevation)+" m", "über dem Meer")
	}
	return c.HTML()
}

func countryName(code string) string {
	switch code {
	case "":
		return ""
	case "DE":
		return "Deutschland"
	case "AT":
		return "Österreich"
	case "CH":
		return "Schweiz"
	}
	return code
}

// adresseForm is "Adresse ändern", filled with what Laib has; Deutschland
// when nothing is set. Below the coordinates the two ways to them.
func adresseForm(z laib.Zuhause) string {
	country := z.Country
	if country == "" {
		country = "DE"
	}
	elevation := ""
	if z.Elevation != nil {
		elevation = strconv.Itoa(*z.Elevation)
	}
	field := func(label, name, value, attrs string) string {
		return "<label>" + label + ` <input name=` + name + ` value="` + html.EscapeString(value) + `" ` + attrs + "></label>"
	}
	query := strings.Join(strings.Fields(z.Street+" "+z.Postcode+" "+z.City), " ")
	var b strings.Builder
	b.WriteString("<section class=karte aria-labelledby=a-neu><h2 id=a-neu>Adresse ändern</h2><form method=post action=" + adresseSet + " class=adresse-form>")
	b.WriteString(field("Straße und Hausnummer", "street", z.Street, "autocomplete=street-address maxlength=120"))
	b.WriteString("<div class=zwei>" + field("PLZ", "postcode", z.Postcode, "autocomplete=postal-code inputmode=numeric maxlength=10") +
		field("Ort", "city", z.City, "autocomplete=address-level2 maxlength=80") + "</div>")
	b.WriteString(field("Land", "country", country, "autocomplete=country maxlength=2 size=2 class=kurz") + "<p class=hilfe>Zwei Buchstaben: DE, AT, CH</p>")
	b.WriteString("<fieldset><legend>Koordinaten</legend><div class=zwei>" +
		field("Breite", "latitude", num(z.Latitude), "inputmode=decimal placeholder=52.1234 maxlength=12") +
		field("Länge", "longitude", num(z.Longitude), "inputmode=decimal placeholder=8.0456 maxlength=12") + "</div>")
	b.WriteString(`<div class=wege><button type=button class=hier hidden>Position aus diesem Gerät übernehmen</button>` +
		`<button type=submit form=osm formtarget=_blank class=osm>Auf OpenStreetMap nachsehen ↗</button></div>` +
		`<p class=hilfe aria-live=polite id=a-wo>Die Position fragt der Browser; sie bleibt in diesem Formular, bis du vorschlägst. OpenStreetMap bekommt die getippte Adresse erst, wenn du dort tippst, in einem neuen Tab: lange drücken oder Rechtsklick auf das Haus, „Adresse anzeigen“, und Breite und Länge abschreiben.</p></fieldset>`)
	b.WriteString(field("Höhe in Metern <span class=k>(freiwillig)</span>", "elevation", elevation, "type=number step=1 min=-500 max=9000 inputmode=numeric class=kurz"))
	b.WriteString("<button class=senden>Vorschlagen</button><p class=hilfe>Die Box ändert nichts, bevor du den Vorschlag in der Inbox freigibst.</p></form>")
	b.WriteString(`<form id=osm method=get action=https://www.openstreetmap.org/search target=_blank rel="noreferrer noopener"><input type=hidden name=query value="` + html.EscapeString(query) + `"></form>`)
	b.WriteString(adresseScript + "</section>")
	return b.String()
}

// adresseScript fills the coordinates from this device (the browser asks
// the person; only over https) and hands OpenStreetMap what is typed now.
// Without it the form is a plain post, and OpenStreetMap gets the address
// Laib has.
const adresseScript = `<script>(function(){
var f=document.querySelector('.adresse-form'),o=document.getElementById('osm'),say=document.getElementById('a-wo');if(!f||!o)return;
function v(n){return (f.elements[n].value||'').trim()}
o.addEventListener('submit',function(){o.elements.query.value=[v('street'),v('postcode'),v('city')].filter(Boolean).join(' ')});
var h=f.querySelector('.hier');if(!navigator.geolocation||!window.isSecureContext)return;h.hidden=false;
h.addEventListener('click',function(){h.disabled=true;say.textContent='Der Browser fragt nach der Position …';
navigator.geolocation.getCurrentPosition(function(p){f.elements.latitude.value=p.coords.latitude.toFixed(4);f.elements.longitude.value=p.coords.longitude.toFixed(4);h.disabled=false;
say.textContent='Übernommen, auf etwa '+Math.max(1,Math.round(p.coords.accuracy))+' m genau. Steht das Gerät nicht im Haus, lieber auf OpenStreetMap nachsehen.'},
function(){h.disabled=false;say.textContent='Das Gerät gibt seine Position nicht her. Auf OpenStreetMap nachsehen geht immer.'},{enableHighAccuracy:true,timeout:15000,maximumAge:0})})})();</script>`

// AdresseVorschlagen hands the form to laib_zuhause as one set: the whole
// block, the empty fields left out (Laib removes them).
func AdresseVorschlagen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, adressePath, http.StatusSeeOther)
			return
		}
		if !adminOnly(w, r) {
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Das Formular ist nicht lesbar.", http.StatusBadRequest)
			return
		}
		say := func(key, nr string) {
			to := adressePath + "?gesagt=" + url.QueryEscape(key)
			if nr != "" {
				to += "&vorschlag=" + nr
			}
			http.Redirect(w, r, to, http.StatusSeeOther)
		}
		fields := map[string]string{}
		for _, k := range zuhauseFields {
			v := oneLine(r.PostForm.Get(k))
			switch k {
			case "country":
				v = strings.ToUpper(v)
			case "latitude", "longitude", "elevation":
				v = strings.ReplaceAll(strings.ReplaceAll(v, " ", ""), ",", ".")
			}
			if v != "" {
				fields[k] = v
			}
		}
		if (fields["latitude"] == "") != (fields["longitude"] == "") {
			say("koordinaten", "")
			return
		}
		me := r.Header.Get("Remote-User")
		if cfg.Laib == nil {
			say("kein-laib", "")
			return
		}
		p, err := cfg.Laib.ZuhauseSet(r.Context(), fields)
		if err != nil {
			key := zuhauseRefusal(err)
			log.Printf("haus: adresse: %s schlägt vor: abgelehnt (%s)", me, key) // laib's words carry the values: not logged
			say(key, "")
			return
		}
		log.Printf("haus: adresse: %s schlägt vor: Vorschlag #%d", me, p.Proposal)
		nr := ""
		if p.Proposal > 0 {
			nr = strconv.Itoa(p.Proposal)
		}
		say("vorgeschlagen", nr)
	}
}

var (
	refusedFlag  = regexp.MustCompile(`flag -(\w+)`)
	refusedField = regexp.MustCompile(`zuhause: (street|postcode|city|country|latitude|longitude|elevation)\b`)
)

// zuhauseRefusal is laib's refusal as a key of adresseSaid. A flag that
// does not parse names itself ("invalid value … for flag -elevation"),
// the state's check names the field first ("box.yml zuhause: postcode …").
func zuhauseRefusal(err error) string {
	if errors.Is(err, laib.ErrNoTool) {
		return "kein-laib"
	}
	text := err.Error()
	field := ""
	if m := refusedFlag.FindStringSubmatch(text); m != nil {
		field = m[1]
	} else if m := refusedField.FindStringSubmatch(text); m != nil {
		field = m[1]
	}
	switch field {
	case "postcode":
		return "plz"
	case "country":
		return "land"
	case "latitude", "longitude":
		return "koordinaten"
	case "elevation":
		return "hoehe"
	case "street", "city":
		return "zeile"
	}
	return "nicht-moeglich"
}

const adresseStyle = `<style>.adresse-form{display:block!important;background:var(--card);border:1px solid var(--edge);border-radius:14px;padding:4px 14px 14px}
.adresse-form label{display:block;margin:12px 0 0;font-size:.85rem;color:var(--muted)}
.adresse-form input{display:block;width:100%;margin-top:4px;font:inherit;font-size:1rem;color:var(--ink);background:var(--bg);border:1px solid var(--edge);border-radius:10px;padding:0 12px;min-height:44px}
.adresse-form input:focus{outline:2px solid var(--accent);outline-offset:1px}.adresse-form input.kurz{width:7rem}
.adresse-form .zwei{display:grid;grid-template-columns:minmax(0,2fr) minmax(0,3fr);gap:0 10px}.adresse-form fieldset .zwei{grid-template-columns:1fr 1fr}
.adresse-form fieldset{border:0;border-top:1px solid var(--line);margin:16px 0 0;padding:6px 0 0}.adresse-form legend{padding:0;margin:10px 0 0;font-size:.72rem;font-weight:600;letter-spacing:.1em;text-transform:uppercase;color:var(--muted)}
.adresse-form .wege{display:flex;flex-wrap:wrap;gap:8px;margin:12px 0 0}.adresse-form .wege button{background:var(--panel);border:1px solid var(--edge);color:var(--ink);white-space:normal;text-align:left}
.adresse-form .hilfe{margin:6px 2px 0;font-size:.8rem;color:var(--muted)}.adresse-form .senden{margin-top:18px;min-height:44px;padding:0 22px;background:var(--accent);color:#fff}
.gesagt{margin:16px 0 0;padding:10px 14px;border-left:3px solid var(--ok);background:var(--card);border-radius:0 10px 10px 0}.gesagt.nein{border-left-color:var(--warn)}.gesagt a{font-weight:600}
#osm{display:none}</style>`
