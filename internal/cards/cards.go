// Package cards is Homes' one card: a title, rows and an optional link
// (laib DEKONSTRUKTION, "Homes: the pages and the cards"). Pages render it
// as HTML; the widgets' /napi JSON and the Bote's replies get the same
// value later. A card shows what there is and says nothing otherwise.
package cards

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Row is one line: what it is, a quieter detail, and where it leads, if
// anywhere (a note's page, the calendar). A row in a time card has its
// When; any row may carry one Action.
type Row struct {
	Text   string  `json:"text"`
	Detail string  `json:"detail"`
	Href   string  `json:"href,omitempty"`
	When   string  `json:"when,omitempty"`
	Action *Action `json:"action,omitempty"`
	Pin    *Pin    `json:"pin,omitempty"`
	// Also are more buttons after Action (a player's Leiser, Lauter,
	// haus#12); when Also is set, even empty, the row puts its buttons on
	// a line of their own
	Also []*Action `json:"also,omitempty"`
	// Slider is a level to set, 0–100 % (haus#64): a dimmer's brightness, a
	// cover's position; drawn on a line of its own under the row's buttons
	Slider *Slider `json:"slider,omitempty"`
	// Color is the row's calendar colour (#rrggbb, haus#66), a dot before
	// the text; never the only sign, the detail names the calendar
	Color string `json:"color,omitempty"`
}

// Slider is a row's level (haus#64): the "60 %" in the row's line is a
// button that opens a sheet (owner, 2026-11-03: a slider on the card moved
// when the page was scrolled) with a large slider, sent when the finger
// lets go, and Steps, each sent on a tap. Label names what it sets, Name
// the thing: the sheet's title and the slider's name for a screen reader.
type Slider struct {
	Label  string `json:"label"`
	Name   string `json:"name"`
	Entity string `json:"entity"`
	Do     string `json:"do"`
	Back   string `json:"back"`
	Value  int    `json:"value"`
	Steps  []Step `json:"steps,omitempty"`
	// Off is what the line says at level 0 ("aus": a lamp that is off,
	// owner 2026-11-04); the button and the sheet say it for "0 %"
	Off string `json:"off,omitempty"`
	// Stopp is a cover's stop, after the steps (owner, 2026-11-04: the
	// app's sheet has Auf, ½, Zu, Stopp); nil, none
	Stopp *Action `json:"stopp,omitempty"`
}

// Step is one quick level in a slider's sheet: "50 %", "Zu".
type Step struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

// Action is a row's one button (haus#35): a form that POSTs to
// SwitchPath — no script needed. Do is the HA service ("light.turn_off")
// or a page's own word ("alle-lichter-aus"); Back is where the page
// lands after. A Switch is drawn as a switch, On its state, Label its
// name for a screen reader; else Label is the button's words, or, with
// Bild, its name beside a picture the class draws (▲ ■ ▼). With a Frage
// the button asks first, and only the question's Ja posts, bestaetigt.
type Action struct {
	Label  string `json:"label"`
	Entity string `json:"entity,omitempty"`
	Do     string `json:"do"`
	Back   string `json:"back"`
	Switch bool   `json:"switch,omitempty"`
	On     bool   `json:"on,omitempty"`
	Bild   string `json:"bild,omitempty"`
	Frage  *Frage `json:"frage,omitempty"`
}

// Frage is what an Action asks before it is sent (a garage opening, as the
// app asks): the question, its yes and its no.
type Frage struct {
	Text string `json:"text"`
	Ja   string `json:"ja"`
	Nein string `json:"nein"`
}

// Pin is a row's ☆ (haus#12): a form that POSTs to the thing's own page
// and puts it on the person's Start, or, On, takes it off; Name is the
// thing's name for a screen reader, Back where the page lands after.
type Pin struct {
	Entity string `json:"entity"`
	Name   string `json:"name"`
	On     bool   `json:"on,omitempty"`
	Back   string `json:"back"`
}

// Link is where the card leads.
type Link struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

// How a card draws its rows: a list in one box (the default), each row a
// box of its own, or a time column first.
const (
	List  = ""
	Boxes = "einzeln"
	Times = "zeiten"
)

// Card is one card.
type Card struct {
	Title string `json:"title"`
	Rows  []Row  `json:"rows"`
	Link  *Link  `json:"link,omitempty"`
	Look  string `json:"look,omitempty"`
	// Note is one quiet line under the title (how the card fills, haus#64),
	// leading to NoteHref when set
	Note     string `json:"note,omitempty"`
	NoteHref string `json:"note_href,omitempty"`
}

// Empty is a card with nothing to say.
func (c Card) Empty() bool { return len(c.Rows) == 0 }

// Add appends a row.
func (c *Card) Add(text, detail string) { c.Rows = append(c.Rows, Row{Text: text, Detail: detail}) }

// AddLink appends a row that leads to href.
func (c *Card) AddLink(text, detail, href string) {
	c.Rows = append(c.Rows, Row{Text: text, Detail: detail, Href: href})
}

// MarshalJSON keeps null out: no rows is [] (Android reads null as "null").
func (c Card) MarshalJSON() ([]byte, error) {
	type plain Card
	if c.Rows == nil {
		c.Rows = []Row{}
	}
	return json.Marshal(plain(c))
}

// ID is the card's element id, from its title: karte-<slug>.
func (c Card) ID() string {
	r := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "ae", "Ö", "oe", "Ü", "ue", "ß", "ss")
	var b strings.Builder
	dash := false
	for _, ch := range r.Replace(strings.ToLower(c.Title)) {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(ch)
			dash = false
		} else {
			dash = true
		}
	}
	if b.Len() == 0 {
		return "karte"
	}
	return "karte-" + b.String()
}

// HTML renders the card as a labelled section; "" when it has no rows.
// Whatever a row says is text, never markup.
func (c Card) HTML() string {
	if c.Empty() {
		return ""
	}
	id := c.ID()
	var b strings.Builder
	class := "karte"
	if c.Look != List {
		class = `"karte ` + html.EscapeString(c.Look) + `"`
	}
	b.WriteString("<section class=" + class + " aria-labelledby=" + id + "><h2 id=" + id + ">" + html.EscapeString(c.Title) + "</h2>")
	switch {
	case c.Note != "" && c.NoteHref != "":
		b.WriteString(`<p class=hinweis><a href="` + html.EscapeString(c.NoteHref) + `">` + html.EscapeString(c.Note) + "</a></p>")
	case c.Note != "":
		b.WriteString("<p class=hinweis>" + html.EscapeString(c.Note) + "</p>")
	}
	b.WriteString("<ul>")
	for _, r := range c.Rows {
		switch {
		case c.Look == Times && r.When != "":
			b.WriteString("<li><time>" + html.EscapeString(r.When) + "</time><div>")
			what(&b, r)
			b.WriteString("</div></li>")
		case r.Also != nil && (r.Action != nil || len(r.Also) > 0):
			b.WriteString(`<li class="tu viele"><div>`)
			what(&b, r)
			b.WriteString("</div><div class=knoepfe>")
			if r.Action != nil {
				b.WriteString(r.Action.Form())
			}
			for _, a := range r.Also {
				b.WriteString(a.Form())
			}
			if r.Pin != nil {
				b.WriteString(r.Pin.form()) // the ☆ last, as on any row
			}
			b.WriteString("</div></li>")
		case r.Action != nil && r.Pin == nil:
			b.WriteString("<li class=tu><div>")
			what(&b, r)
			b.WriteString("</div>" + r.Action.Form() + "</li>")
		case r.Pin != nil:
			b.WriteString("<li class=tu><div>")
			what(&b, r)
			b.WriteString("</div><div class=knoepfe>")
			if r.Action != nil {
				b.WriteString(r.Action.Form())
			}
			b.WriteString(r.Pin.form() + "</div></li>") // the ☆ last: one column down the card
		case c.Look == Times: // a line across, not a time
			b.WriteString("<li class=voll>")
			what(&b, r)
			b.WriteString("</li>")
		default:
			b.WriteString("<li>")
			what(&b, r)
			b.WriteString("</li>")
		}
	}
	b.WriteString("</ul>")
	if c.Link != nil && c.Link.Href != "" {
		b.WriteString(`<p><a href="` + html.EscapeString(c.Link.Href) + `">` + html.EscapeString(c.Link.Text) + "</a></p>")
	}
	b.WriteString("</section>")
	return b.String()
}

// what is a row's text, leading where it leads, and its detail. A row
// with a Slider has its level ("60 %") as the button that opens the
// slider's sheet: in the detail, else in a text that leads nowhere, else
// after the detail; the sheet follows, hidden until then.
func what(b *strings.Builder, r Row) {
	if colorRE.MatchString(r.Color) {
		b.WriteString(`<span class=farbe style="background:` + r.Color + `" aria-hidden=true></span>`)
	}
	text, detail := html.EscapeString(r.Text), html.EscapeString(r.Detail)
	if s := r.Slider; s != nil {
		var ok bool
		if detail, ok = s.inline(r.Detail); !ok {
			if text, ok = s.inline(r.Text); !ok || r.Href != "" {
				text = html.EscapeString(r.Text)
				detail = strings.TrimSpace(html.EscapeString(r.Detail) + map[bool]string{true: " · "}[r.Detail != ""] + s.Button())
			}
		}
	}
	if r.Href != "" {
		b.WriteString(`<a href="` + html.EscapeString(r.Href) + `">` + text + "</a>")
	} else {
		b.WriteString(text)
	}
	if detail != "" {
		b.WriteString(" <span class=k>" + detail + "</span>")
	}
	if r.Slider != nil {
		b.WriteString(r.Slider.Sheet())
	}
}

// Form is the action's button: a POST to SwitchPath with what to do. With
// a Frage, a button that opens the question (a popover, so without script
// too), whose Ja posts with bestaetigt.
func (a *Action) Form() string {
	laeuft := `data-laeuft="Schaltet …"`
	if a.Bild != "" {
		laeuft = `data-laeuft=""` // a picture stays; the tile says „schaltet …“
	}
	switch {
	case a.Frage != nil:
		id := a.FrageID()
		return a.button(`type=button popovertarget=`+id) + `<div popover id=` + id + ` class=blatt role=dialog aria-labelledby=` + id + `-titel><p class=blatt-titel id=` + id + `-titel>` +
			html.EscapeString(a.Frage.Text) + `</p><div class=schritte>` + a.post(`<input type=hidden name=bestaetigt value=ja><button data-laeuft="…">`+html.EscapeString(a.Frage.Ja)+`</button>`) +
			`<button type=button popovertarget=` + id + ` popovertargetaction=hide>` + html.EscapeString(a.Frage.Nein) + `</button></div></div>`
	case a.Switch:
		return a.post(`<button class=schalter role=switch aria-checked=` + strconv.FormatBool(a.On) + ` aria-label="` + html.EscapeString(a.Label) + `"></button>`)
	}
	return a.post(a.button(laeuft))
}

// FrageID is the element id of the action's question.
func (a *Action) FrageID() string { return "frage-" + slug(a.Entity) + "-" + slug(a.Do) }

// post is the action's form around button.
func (a *Action) post(button string) string {
	var b strings.Builder
	b.WriteString("<form method=post action=" + SwitchPath + ">")
	if a.Entity != "" {
		b.WriteString(`<input type=hidden name=geraet value="` + html.EscapeString(a.Entity) + `">`)
	}
	b.WriteString(`<input type=hidden name=tun value="` + html.EscapeString(a.Do) + `">`)
	b.WriteString(`<input type=hidden name=zurueck value="` + html.EscapeString(a.Back) + `">`)
	return b.String() + button + "</form>"
}

// button is the action's visible button, with attrs: its words, or its picture named.
func (a *Action) button(attrs string) string {
	if a.Bild != "" {
		return `<button ` + attrs + ` class=` + html.EscapeString(a.Bild) + ` aria-label="` + html.EscapeString(a.Label) + `"></button>`
	}
	return `<button ` + attrs + `>` + html.EscapeString(a.Label) + "</button>"
}

// slug is an entity id or a service as part of an element id.
func slug(s string) string {
	return strings.NewReplacer(".", "-", "_", "-").Replace(strings.ToLower(s))
}

// percent is a level as the page says it.
func percent(v int) string { return strconv.Itoa(max(0, min(v, 100))) + " %" }

// says is the slider's level as the line says it: "60 %", or Off at 0.
func (s *Slider) says() string {
	if s.Value <= 0 && s.Off != "" {
		return s.Off
	}
	return percent(s.Value)
}

// ID is the slider's sheet's element id, from its thing.
func (s *Slider) ID() string { return "regler-" + slug(s.Entity) }

// Button is the level that opens the sheet: a popover's button, so it
// opens without script too, and says aria-expanded on its own.
func (s *Slider) Button() string {
	return `<button type=button class=stufe popovertarget=` + s.ID() + ` aria-haspopup=dialog aria-label="` +
		html.EscapeString(s.Name+" "+s.Label+" "+s.says()+", einstellen") + `">` + html.EscapeString(s.says()) + `</button>`
}

// inline is line, escaped, with its last "60 %" (or "aus", a word of its
// own) as the button; !ok when the line does not say the level.
func (s *Slider) inline(line string) (string, bool) {
	w := s.says()
	i := strings.LastIndex(line, w)
	if i < 0 {
		return html.EscapeString(line), false
	}
	before, _ := utf8.DecodeLastRuneInString(line[:i])
	if after, _ := utf8.DecodeRuneInString(line[i+len(w):]); unicode.IsLetter(before) || unicode.IsDigit(before) || unicode.IsLetter(after) {
		return html.EscapeString(line), false
	}
	return html.EscapeString(line[:i]) + s.Button() + html.EscapeString(line[i+len(w):]), true
}

// hidden are a slider form's fields but wert.
func (s *Slider) hidden() string {
	return `<input type=hidden name=geraet value="` + html.EscapeString(s.Entity) + `"><input type=hidden name=tun value="` + html.EscapeString(s.Do) +
		`"><input type=hidden name=zurueck value="` + html.EscapeString(s.Back) + `">`
}

// Sheet is the slider's sheet: the thing's name, its level large, the
// slider (a POST to SwitchPath with wert, sent when the finger lets go),
// the Steps (each its own POST), Stopp and Fertig. A popover: a tap outside,
// Escape and the phone's back close it, with or without script.
func (s *Slider) Sheet() string {
	id, v := s.ID(), strconv.Itoa(max(0, min(s.Value, 100)))
	var b strings.Builder
	b.WriteString(`<div popover id=` + id + ` class=blatt role=dialog aria-labelledby=` + id + `-titel>`)
	b.WriteString(`<p class=blatt-titel id=` + id + `-titel>` + html.EscapeString(s.Name) + `</p>`)
	b.WriteString(`<form method=post action=` + SwitchPath + ` class=regler>` + s.hidden())
	b.WriteString(`<p class=blatt-wert><span class=k>` + html.EscapeString(s.Label) + `</span> <output>` + html.EscapeString(s.says()) + `</output></p>`)
	b.WriteString(`<input type=range name=wert min=0 max=100 step=1 value=` + v + ` aria-label="` + html.EscapeString(s.Name+" "+s.Label) +
		`" oninput="this.form.querySelector('output').value=this.value+' %'" onchange="this.form.requestSubmit?this.form.requestSubmit():this.form.submit()">`)
	b.WriteString(`<noscript><button data-laeuft="Setzt …">Setzen</button></noscript></form>`)
	if len(s.Steps) > 0 || s.Stopp != nil {
		b.WriteString(`<div class=schritte>`)
		for _, st := range s.Steps {
			b.WriteString(`<form method=post action=` + SwitchPath + `>` + s.hidden() + `<input type=hidden name=wert value=` + strconv.Itoa(max(0, min(st.Value, 100))) +
				`><button data-laeuft="…" aria-label="` + html.EscapeString(s.Label+" "+st.Label) + `">` + html.EscapeString(st.Label) + `</button></form>`)
		}
		if s.Stopp != nil {
			b.WriteString(s.Stopp.Form())
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`<button type=button class=fertig popovertarget=` + id + ` popovertargetaction=hide>Fertig</button></div>`)
	return b.String()
}

// form is the ☆: a POST to the thing's own page, stern an or aus.
func (p *Pin) form() string {
	value, star, says := "an", "☆", " auf Start zeigen"
	if p.On {
		value, star, says = "aus", "★", " von Start nehmen"
	}
	return `<form method=post action="` + html.EscapeString(ThingPage(p.Entity)) + `"><input type=hidden name=stern value=` + value +
		`><input type=hidden name=zurueck value="` + html.EscapeString(p.Back) + `"><button class="anheften` + map[bool]string{true: " an"}[p.On] +
		`" data-laeuft="…" aria-label="` + html.EscapeString(p.Name+says) + `">` + star + "</button></form>"
}

// Style is the cards' CSS, for a page's head: the house palette's
// variables (the page frame sets them), a row a finger hits as a whole.
var colorRE = regexp.MustCompile(`^#[0-9a-f]{6}$`)

const Style = `<style>.farbe{display:inline-block;width:.65em;height:.65em;border-radius:50%;margin-right:.45em;vertical-align:.05em}.karte{margin:22px 0 0}
.karte h2{font-size:.95rem;font-weight:700;color:var(--ink);margin:0 0 8px}
.karte ul{list-style:none;margin:0;padding:0;background:var(--card);border-radius:14px}
.karte li{position:relative;padding:11px 14px;border-top:1px solid var(--line);overflow-wrap:anywhere;min-height:44px}.karte li:first-child{border-top:0}
.karte li a{display:block;color:inherit;text-decoration:none}.karte li a::after{content:"";position:absolute;inset:0}
.karte li .k{display:block;font-size:.82rem;margin-top:1px}
.karte li.tu{display:flex;align-items:center;justify-content:space-between;gap:12px}.karte form{position:relative;z-index:1;flex:none;display:flex}.karte form.eingabe,.karte form.notizform,.karte form.rezeptwahl{display:block;flex:auto}
.knoepfe{position:relative;z-index:1;flex:none;display:flex;align-items:center;gap:2px}
.karte li.viele{flex-wrap:wrap;gap:8px}.karte li.viele>div:first-child{flex:1 1 100%}.viele .knoepfe{gap:8px;flex-wrap:wrap}
.anheften{width:44px;min-height:44px;padding:0;background:none;border-radius:50%;color:var(--muted);font-size:1.35rem;font-weight:400;line-height:1}.anheften.an{color:var(--state)}
.einzeln ul{background:none;border:0;border-radius:0;display:grid;gap:6px}.einzeln li,.einzeln li:first-child{background:var(--card);border:0;border-radius:14px}
.schalter{position:relative;width:52px;min-height:40px;padding:0;margin-right:-6px;background:none;border-radius:20px}
.schalter::before{content:"";position:absolute;left:8px;top:10px;width:36px;height:20px;border-radius:10px;background:var(--track)}
.schalter::after{content:"";position:absolute;left:11px;top:13px;width:14px;height:14px;border-radius:50%;background:#fff}
.schalter[aria-checked=true]::before{background:var(--state)}.schalter[aria-checked=true]::after{left:27px}
.karte li button.stufe{position:relative;z-index:1;display:inline-flex;align-items:center;min-height:44px;margin:-12px 0;padding:0 6px;background:none;border-radius:8px;font-size:inherit;color:var(--accent);text-decoration:underline dotted;text-underline-offset:3px;vertical-align:baseline}
.blatt{position:fixed;inset:auto 0 0 0;margin:0 auto;width:min(32rem,100%);max-width:100%;box-sizing:border-box;padding:18px 20px calc(18px + env(safe-area-inset-bottom));border:0;border-radius:22px 22px 0 0;background:var(--panel);color:var(--ink)}
.blatt::backdrop{background:rgba(0,0,0,.55)}.blatt-titel{margin:0 0 4px;font-weight:700;font-size:1.05rem}
.blatt-wert{margin:0 0 6px;display:flex;align-items:baseline;gap:10px}.blatt-wert output{font-size:2.4rem;font-weight:700;font-variant-numeric:tabular-nums;color:var(--ink)}
.blatt form.regler{display:block}.blatt input[type=range]{display:block;width:100%;min-height:48px;margin:0;accent-color:var(--accent)}
.blatt .schritte{display:flex;gap:8px;margin:10px 0 0}.blatt .schritte form,.blatt .schritte>button{flex:1}.blatt .schritte button{width:100%}
.blatt .fertig{display:block;width:100%;margin-top:14px;background:var(--accent);color:#fff}
.karte .hinweis{margin:-2px 2px 8px;font-size:.85rem;color:var(--muted)}.karte .hinweis a{color:var(--accent)}
.zeiten li{display:grid;grid-template-columns:6.4rem 1fr;gap:10px;align-items:baseline}
.zeiten time{font-size:.8rem;color:var(--muted);font-variant-numeric:tabular-nums}.zeiten li.voll{display:block}</style>`
