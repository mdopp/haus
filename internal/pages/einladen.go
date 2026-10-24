package pages

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// What the Box card "Person einladen" says (haus#56, laib#360): an admin
// invites someone new; Laib makes the account in its group and Authelia
// mails the set-password link, or Laib hands the link back when the box
// has no mail. No password passes through Haus.
const (
	invitePath        = "/box/einladen"
	saysInviteWhat    = "Für jemanden, der neu dazukommt: Die Person bekommt ein eigenes Konto für alle Apps der Box und legt ihr Passwort selbst fest. Du siehst es nie."
	saysUserHint      = "Damit meldet sich die Person an: Kleinbuchstaben, ohne Leerzeichen. Leer lassen, dann wird es der Vorname."
	saysEmailHint     = "Dorthin schickt die Box den Link, mit dem die Person ihr Passwort festlegt."
	saysFamilyOnly    = "Haus lädt nur in die Familie ein. Einen weiteren Admin legst du aus Sicherheitsgründen direkt an der Box an: laib person invite <benutzer> … --group admins"
	saysInviteMissing = "Einladen geht auf dieser Box noch nicht: Laib kann es hier noch nicht. Bis zum nächsten Laib-Update legst du neue Personen wie bisher in LLDAP an."
	saysNeedFirst     = "Bitte den Vornamen eintragen."
	saysBadEmail      = "Die E-Mail-Adresse sieht nicht vollständig aus."
	saysBadUser       = "Der Benutzername darf nur Kleinbuchstaben, Ziffern, Punkt, Bindestrich und Unterstrich haben und muss mit einem Buchstaben anfangen."
	saysPendingTitle  = "Eingeladen, noch kein Passwort"
	saysPendingNone   = "Die Liste der Eingeladenen lässt sich gerade nicht lesen."
	saysSignalHint    = "Wenn du magst: mit Ländervorwahl, etwa +49 170 1234567. Dann darf die Person dem Boten auf Signal schreiben. Die Nummer kommt als Vorschlag in Laibs Eingang und gilt, sobald er freigegeben ist."
	saysBadSignal     = "Die Signal-Nummer bitte mit Ländervorwahl, etwa +49 170 1234567."
	saysSignalWaits   = "Signal-Nummer eingetragen: Sie gilt, sobald der Vorschlag freigegeben ist."
	saysSignalLater   = "Signal-Nummer für jemanden, der schon ein Konto hat: Benutzername und Nummer. Die Nummer kommt als Vorschlag in Laibs Eingang."
	saysSignalSet     = "Signal-Nummer für %s eingetragen: Sie gilt, sobald der Vorschlag freigegeben ist."
	saysWelcomeAfter  = "Nach dem ersten Anmelden zeigt Haus die Seite „Willkommen“: welche Apps es gibt und wie man sie einrichtet."
)

// invitation is what the card says after a POST: the result, or what
// went wrong, and the form's values to keep.
type invitation struct {
	done     *laib.Invited
	first    string
	problem  string
	form     laib.Invite
	signal   bool           // a Signal number went with it: say it waits for the owner
	numbered *laib.Proposed // a Signal number set for someone already there (laib#596)
	numFor   string         // whose
	inbox    string         // Laib's page, for the way to the proposal
}

// empty is no POST's outcome: the plain Box page.
func (inv invitation) empty() bool {
	return inv.done == nil && inv.first == "" && inv.problem == "" && inv.form == (laib.Invite{}) && !inv.signal && inv.numbered == nil
}

// signalRE is a Signal number as laib#596 takes it, E.164: + and 7 to 15
// digits, the first not 0.
var signalRE = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)

// signalNumber is what an admin typed, as E.164: spaces, dashes, dots,
// slashes and brackets out, a leading 00 is +. "" stays "".
func signalNumber(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(" -./()\u00a0", r) {
			return -1
		}
		return r
	}, s)
	if strings.HasPrefix(s, "00") {
		s = "+" + s[2:]
	}
	return s
}

var userRE = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,31}$`)

// suggestUser is a user name from a first name: lower case, umlauts
// spelled out, nothing but letters and digits.
func suggestUser(first string) string {
	first = strings.ToLower(strings.TrimSpace(first))
	first = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "é", "e", "è", "e", "à", "a", "ç", "c").Replace(first)
	var b strings.Builder
	for _, c := range first {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' && b.Len() > 0 {
			b.WriteRune(c)
		}
		if c == ' ' || c == '-' {
			break // the first of two first names
		}
	}
	return b.String()
}

// Einladen is the card's form (haus#56): admins only, from Haus's own
// page only. It passes the invitation to Laib and shows the Box page
// again with what Laib did; the link, if any, is in that answer only,
// never in a URL or a log.
func Einladen(cfg BoxConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Redirect(w, r, "/box", http.StatusSeeOther)
			return
		}
		if !isAdmin(r) {
			http.Error(w, "Einladen dürfen nur Admins.", http.StatusForbidden)
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Das geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Das Formular ist nicht lesbar.", http.StatusBadRequest)
			return
		}
		f := r.PostForm
		if f.Get("tun") == "signal" {
			signalLater(w, r, cfg)
			return
		}
		in := laib.Invite{User: strings.TrimSpace(f.Get("benutzer")), First: oneLine(f.Get("vorname")), Last: oneLine(f.Get("nachname")),
			Email: strings.TrimSpace(f.Get("email")), Group: "family", Resend: f.Get("erneut") == "1"}
		if !in.Resend {
			in.Signal = signalNumber(f.Get("signal"))
		}
		inv := invitation{form: in, first: in.First, inbox: cfg.LaibURL}
		if inv.first == "" {
			inv.first = in.User
		}
		if in.User == "" && !in.Resend {
			in.User = suggestUser(in.First)
		}
		switch {
		case in.First == "" && !in.Resend:
			inv.problem = saysNeedFirst
		case !userRE.MatchString(in.User):
			inv.problem = saysBadUser
		case in.Resend:
			in.Group = "" // the account is there; the group stays as it is
		case !strings.Contains(in.Email, "@") || strings.HasSuffix(in.Email, "@") || strings.ContainsAny(in.Email, " \t,;<>"):
			inv.problem = saysBadEmail
		case in.Signal != "" && !signalRE.MatchString(in.Signal):
			inv.problem = saysBadSignal
		}
		if in.Signal != "" && inv.problem == "" {
			if takes, _ := cfg.Laib.Takes(r.Context(), "person_invite", "signal"); !takes {
				in.Signal = "" // a Laib before laib#596: the field was not offered, nothing to pass on
			}
		}
		if inv.problem != "" {
			writeBox(w, r, cfg, http.StatusBadRequest, inv)
			return
		}
		inv.form = in
		got, err := cfg.Laib.PersonInvite(r.Context(), in)
		if err != nil {
			log.Printf("haus: einladen: %s by %s: %v", in.User, r.Header.Get("Remote-User"), err) // no link is in an error
			inv.problem = inviteProblem(err)
			code := http.StatusBadGateway
			if errors.Is(err, laib.ErrNoTool) {
				code = http.StatusNotImplemented
			}
			writeBox(w, r, cfg, code, inv)
			return
		}
		inv.done = &got
		inv.signal = in.Signal != ""
		inv.form = laib.Invite{} // the form is empty again for the next one
		log.Printf("haus: einladen: %s by %s: mailed %v", in.User, r.Header.Get("Remote-User"), got.Mailed)
		writeBox(w, r, cfg, http.StatusOK, inv)
	}
}

// signalLater sets the Signal number of someone who already has an
// account (haus#87, laib#596): Benutzername and Nummer, one proposal; the
// card says it waits for the owner, with the way to it.
func signalLater(w http.ResponseWriter, r *http.Request, cfg BoxConfig) {
	user, number := strings.TrimSpace(r.PostForm.Get("benutzer")), signalNumber(r.PostForm.Get("signal"))
	inv := invitation{inbox: cfg.LaibURL, numFor: user}
	switch {
	case !userRE.MatchString(user):
		inv.problem = saysBadUser
	case !signalRE.MatchString(number):
		inv.problem = saysBadSignal
	}
	if inv.problem != "" {
		writeBox(w, r, cfg, http.StatusBadRequest, inv)
		return
	}
	p, err := cfg.Laib.PersonSignal(r.Context(), user, number)
	if err != nil {
		log.Printf("haus: einladen: signal for %s by %s: %v", user, r.Header.Get("Remote-User"), err)
		inv.problem = inviteProblem(err)
		writeBox(w, r, cfg, http.StatusBadGateway, inv)
		return
	}
	log.Printf("haus: einladen: signal for %s by %s: Vorschlag #%d", user, r.Header.Get("Remote-User"), p.Proposal)
	inv.numbered = &p
	writeBox(w, r, cfg, http.StatusOK, inv)
}

// inviteProblem is Laib's refusal as the admin reads it: its own sentence
// ("anna gibt es schon"), without the tool's name in front.
func inviteProblem(err error) string {
	if errors.Is(err, laib.ErrNoTool) {
		return saysInviteMissing
	}
	msg := err.Error()
	for _, p := range []string{"laib person_invite: ", "laib: "} {
		msg = strings.TrimPrefix(msg, p)
	}
	return "Nicht eingeladen: " + msg
}

const inviteStyle = `<style>.einladen label{display:block;margin:.6rem 0 .15rem}.einladen input,.einladen select{font:inherit;width:100%;max-width:26rem;padding:.35rem .5rem;border:1px solid #c9c3b6;border-radius:.4rem;background:#fff;box-sizing:border-box}
.einladen .k{display:block;font-size:.85rem}.einladen button{margin-top:.8rem}.erfolg{border-left:3px solid #2b7a3d;padding:.2rem .7rem;margin:.6rem 0}.problem{border-left:3px solid #b3261e;padding:.2rem .7rem;margin:.6rem 0}
.link{display:flex;gap:.4rem;flex-wrap:wrap;max-width:40rem}.link input{flex:1;min-width:12rem;font:13px ui-monospace,Menlo,Consolas,monospace;padding:.35rem .5rem;border:1px solid #c9c3b6;border-radius:.4rem}</style>`

// inviteCard is the Box card: what Laib just did, the form, and who is
// invited but has no password yet, each with "Erneut senden".
func inviteCard(ctx context.Context, c *laib.Client, inv invitation) string {
	var b strings.Builder
	b.WriteString(inviteStyle + "<h2 id=einladen>Person einladen</h2>")
	pending, perr := c.PersonPending(ctx)
	if errors.Is(perr, laib.ErrNoTool) {
		b.WriteString("<p class=k>" + html.EscapeString(saysInviteMissing) + "</p>")
		return b.String()
	}
	if inv.done != nil {
		b.WriteString(inviteDone(*inv.done, inv.first))
		if inv.signal { // the number waits for the owner (laib#596, laib#588)
			b.WriteString("<p class=erfolg role=status>" + html.EscapeString(saysSignalWaits))
			nr := ""
			if inv.done.Proposal > 0 {
				nr = strconv.Itoa(inv.done.Proposal)
			}
			if link := inboxLink(inv.inbox, nr); link != "" {
				b.WriteString(" " + link)
			}
			b.WriteString("</p>")
		}
	}
	if inv.problem != "" {
		b.WriteString("<p class=problem role=alert>" + html.EscapeString(inv.problem) + "</p>")
	}
	if inv.problem == saysInviteMissing {
		return b.String()
	}
	f := inv.form
	if f.Resend { // a failed resend does not fill the form for a new person
		f = laib.Invite{}
	}
	b.WriteString("<p class=k>" + html.EscapeString(saysInviteWhat) + "</p>")
	b.WriteString("<form class=einladen method=post action=" + invitePath + ">")
	field := func(id, label, typ, value, extra, hint string) {
		b.WriteString("<label for=" + id + ">" + label + "</label><input id=" + id + " name=" + id + " type=" + typ + " value=\"" + html.EscapeString(value) + "\"" + extra + ">")
		if hint != "" {
			b.WriteString("<span class=k>" + html.EscapeString(hint) + "</span>")
		}
	}
	// the user name follows the first name as a suggestion until the admin types one
	field("vorname", "Vorname", "text", f.First, " required autocomplete=off oninput=\"var u=document.getElementById('benutzer');u.placeholder=this.value.toLowerCase().replace(/ä/g,'ae').replace(/ö/g,'oe').replace(/ü/g,'ue').replace(/ß/g,'ss').split(/[ -]/)[0].replace(/[^a-z0-9]/g,'')\"", "")
	field("nachname", "Nachname", "text", f.Last, " autocomplete=off", "")
	field("benutzer", "Benutzername", "text", f.User, " autocomplete=off autocapitalize=none spellcheck=false pattern=\"[a-z][a-z0-9._\\-]{1,31}\" placeholder=\""+html.EscapeString(suggestUser(f.First))+"\"", saysUserHint)
	field("email", "E-Mail", "email", f.Email, " required autocomplete=off", saysEmailHint)
	if takes, err := c.Takes(ctx, "person_invite", "signal"); takes { // only once Laib takes it (laib#596)
		field("signal", "Signal-Nummer", "tel", f.Signal, " autocomplete=off inputmode=tel placeholder=\"+49 170 1234567\"", saysSignalHint)
	} else if err != nil {
		log.Printf("haus: einladen: tools/list: %v", err)
	}
	b.WriteString("<button>Einladen</button></form><p class=k>" + html.EscapeString(saysFamilyOnly) + "</p>")
	if takes, _ := c.Takes(ctx, "person_signal", "number"); takes { // laib#596
		if p := inv.numbered; p != nil {
			b.WriteString("<p class=erfolg role=status>" + html.EscapeString(fmt.Sprintf(saysSignalSet, inv.numFor)))
			nr := ""
			if p.Proposal > 0 {
				nr = strconv.Itoa(p.Proposal)
			}
			if link := inboxLink(inv.inbox, nr); link != "" {
				b.WriteString(" " + link)
			}
			b.WriteString("</p>")
		}
		b.WriteString("<h3 id=signal-nachtragen>Signal-Nummer nachtragen</h3><p class=k>" + html.EscapeString(saysSignalLater) + "</p>" +
			"<form class=einladen method=post action=" + invitePath + "><input type=hidden name=tun value=signal>" +
			"<label for=signal-benutzer>Benutzername</label><input id=signal-benutzer name=benutzer type=text required autocomplete=off autocapitalize=none spellcheck=false pattern=\"[a-z][a-z0-9._\\-]{1,31}\">" +
			"<label for=signal-nummer>Signal-Nummer</label><input id=signal-nummer name=signal type=tel required autocomplete=off inputmode=tel placeholder=\"+49 170 1234567\">" +
			"<button>Nummer eintragen</button></form>")
	}
	switch {
	case perr != nil:
		log.Printf("haus: einladen: pending: %v", perr)
		b.WriteString("<p class=k>" + html.EscapeString(saysPendingNone) + "</p>")
	case len(pending) > 0:
		b.WriteString("<h3>" + html.EscapeString(saysPendingTitle) + "</h3><table>")
		for _, p := range pending {
			name := p.Name
			if name == "" {
				name = p.User
			}
			first, last, _ := strings.Cut(name, " ")
			since := ""
			if t, err := time.Parse(time.RFC3339, p.InvitedAt); err == nil {
				since = "eingeladen am " + dayWords(t, time.Local)
			}
			b.WriteString("<tr><td>" + html.EscapeString(name) + "<br><span class=k>" + html.EscapeString(p.User+" · "+p.Email) + "</span></td><td class=k>" + html.EscapeString(since) + "</td><td>" +
				"<form method=post action=" + invitePath + "><input type=hidden name=erneut value=1>" +
				"<input type=hidden name=benutzer value=\"" + html.EscapeString(p.User) + "\"><input type=hidden name=email value=\"" + html.EscapeString(p.Email) + "\">" +
				"<input type=hidden name=vorname value=\"" + html.EscapeString(first) + "\"><input type=hidden name=nachname value=\"" + html.EscapeString(last) + "\">" +
				"<button aria-label=\"Link an " + html.EscapeString(name) + " erneut senden\">Erneut senden</button></form></td></tr>")
		}
		b.WriteString("</table>")
	}
	return b.String()
}

// inviteDone says what Laib did: its own sentence, else Haus's; without
// mail the link to pass on, with a copy button, and how long it lasts.
func inviteDone(got laib.Invited, first string) string {
	if first == "" {
		first = got.User
	}
	say := strings.TrimSpace(got.Message)
	if say == "" {
		if got.Mailed {
			say = first + " bekommt eine Mail mit einem Link, um das eigene Passwort festzulegen."
		} else {
			say = "Auf dieser Box ist kein Mailversand eingerichtet. Schick " + first + " den Link unten, zum Beispiel per Signal."
		}
	}
	var b strings.Builder
	b.WriteString("<div class=erfolg role=status><p><b>" + html.EscapeString(say) + "</b></p>")
	if got.Link != "" {
		b.WriteString("<p>Mit diesem Link legt " + html.EscapeString(first) + " das eigene Passwort fest:</p><div class=link><input id=einladelink readonly value=\"" + html.EscapeString(got.Link) + "\" onfocus=\"this.select()\" aria-label=\"Link für " + html.EscapeString(first) + "\">" +
			"<button type=button onclick=\"var l=document.getElementById('einladelink');l.select();(navigator.clipboard?navigator.clipboard.writeText(l.value):Promise.reject()).then(()=>{this.textContent='Kopiert'},()=>{document.execCommand('copy');this.textContent='Kopiert'})\">Link kopieren</button></div>")
	}
	if life := lifetime(got.Expires); life != "" {
		b.WriteString("<p class=k>Der Link gilt " + html.EscapeString(life) + ". Danach hier unten „Erneut senden“.</p>")
	}
	b.WriteString("<p class=k>Danach meldet sich " + html.EscapeString(first) + " mit dem Benutzernamen „" + html.EscapeString(got.User) + "“ an. " + html.EscapeString(saysWelcomeAfter) + "</p></div>")
	return b.String()
}

// lifetime is how long a link lasts, as the admin says it: "bis
// 19. Oktober 2026, 18:30" for a time, "24 Stunden" for a duration,
// Laib's own words otherwise.
func lifetime(expires string) string {
	expires = strings.TrimSpace(expires)
	if expires == "" {
		return ""
	}
	if t, err := time.Parse(time.RFC3339, expires); err == nil {
		t = t.Local()
		return "bis " + dayWords(t, time.Local) + ", " + t.Format("15:04") + " Uhr"
	}
	if d, err := time.ParseDuration(expires); err == nil && d > 0 {
		switch {
		case d%(24*time.Hour) == 0 && d >= 48*time.Hour:
			return "noch " + strconv.Itoa(int(d/(24*time.Hour))) + " Tage"
		case d%time.Hour == 0:
			if d == time.Hour {
				return "noch eine Stunde"
			}
			return "noch " + strconv.Itoa(int(d/time.Hour)) + " Stunden"
		default:
			return "noch " + strconv.Itoa(int(d.Round(time.Minute)/time.Minute)) + " Minuten"
		}
	}
	return expires
}
