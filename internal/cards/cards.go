// Package cards is Haus's one card: a title, rows and an optional link
// (laib DEKONSTRUKTION, "Haus: the pages and the cards"). Pages render it
// as HTML; the widgets' /napi JSON and the Bote's replies get the same
// value later. A card shows what there is and says nothing otherwise.
package cards

import (
	"encoding/json"
	"html"
	"strings"
)

// Row is one line: what it is, a quieter detail, and where it leads, if
// anywhere (a note's page, the calendar).
type Row struct {
	Text   string `json:"text"`
	Detail string `json:"detail"`
	Href   string `json:"href,omitempty"`
}

// Link is where the card leads.
type Link struct {
	Href string `json:"href"`
	Text string `json:"text"`
}

// Card is one card.
type Card struct {
	Title string `json:"title"`
	Rows  []Row  `json:"rows"`
	Link  *Link  `json:"link,omitempty"`
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
func (c Card) HTML() string {
	if c.Empty() {
		return ""
	}
	id := c.ID()
	var b strings.Builder
	b.WriteString("<section class=karte aria-labelledby=" + id + "><h2 id=" + id + ">" + html.EscapeString(c.Title) + "</h2><ul>")
	for _, r := range c.Rows {
		if r.Href != "" {
			b.WriteString(`<li><a href="` + html.EscapeString(r.Href) + `">` + html.EscapeString(r.Text) + "</a>")
		} else {
			b.WriteString("<li>" + html.EscapeString(r.Text))
		}
		if r.Detail != "" {
			b.WriteString(" <span class=k>" + html.EscapeString(r.Detail) + "</span>")
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
	if c.Link != nil && c.Link.Href != "" {
		b.WriteString(`<p><a href="` + html.EscapeString(c.Link.Href) + `">` + html.EscapeString(c.Link.Text) + "</a></p>")
	}
	b.WriteString("</section>")
	return b.String()
}

// Style is the cards' CSS, for a page's head.
const Style = `<style>.karte{background:#fff;border:1px solid #e6e2d8;border-radius:.6rem;padding:.4rem 1rem .6rem;margin:1rem 0}
.karte h2{margin:.4rem 0}.karte ul{list-style:none;margin:0;padding:0}.karte li{padding:.3rem 0;border-bottom:1px solid #f0ede5}.karte li:last-child{border-bottom:0}</style>`
