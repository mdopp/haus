package merken

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Seite is what a page says of itself.
type Seite struct {
	URL          string
	Titel        string
	Beschreibung string
	Site         string
	Text         string // its readable text, paragraphs apart, cut to maxText
	Rezept       *Rezept
	Album        *Album
	Reise        *Reise
}

// Rezept is a page's schema.org Recipe.
type Rezept struct {
	Name, Portionen, Zeit, Beschreibung string
	Zutaten, Schritte                   []string
}

// Album is a review of a music album (schema.org Review of a MusicAlbum),
// or the album alone.
type Album struct {
	Band, Name, Jahr, Wertung, Rezension string
}

// Reise is a place to stay or an offer (Hotel, LodgingBusiness,
// TouristTrip, an Offer's item).
type Reise struct {
	Name, Ort, Anbieter, Preis, Beschreibung string
}

const maxText = 20000

var (
	reLD     = regexp.MustCompile(`(?is)<script[^>]*type\s*=\s*["']?application/ld\+json["']?[^>]*>(.*?)</script>`)
	reMeta   = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	reAttr   = regexp.MustCompile(`(?is)([a-z:-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
	reTitle  = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reDrop   = regexp.MustCompile(`(?is)<(script|style|noscript|svg|nav|header|footer|aside|form|iframe|template)\b.*?</(script|style|noscript|svg|nav|header|footer|aside|form|iframe|template)>`)
	reBlock  = regexp.MustCompile(`(?i)<(/?(p|div|br|li|h[1-6]|section|article|tr|blockquote|figcaption)\b[^>]*)>`)
	reTag    = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpaces = regexp.MustCompile(`[ \t\x{a0}]+`)
	reISO    = regexp.MustCompile(`^P(?:(\d+)D)?T?(?:(\d+)H)?(?:(\d+)M)?`)
)

// Lesen reads a fetched page.
func Lesen(pageURL, page string) Seite {
	s := Seite{URL: pageURL}
	meta := map[string]string{}
	for _, m := range reMeta.FindAllString(page, -1) {
		a := attrs(m)
		key := strings.ToLower(a["property"])
		if key == "" {
			key = strings.ToLower(a["name"])
		}
		if key != "" && a["content"] != "" {
			if _, seen := meta[key]; !seen {
				meta[key] = clean(a["content"])
			}
		}
	}
	s.Titel = first(meta["og:title"], meta["twitter:title"])
	if s.Titel == "" {
		if m := reTitle.FindStringSubmatch(page); m != nil {
			s.Titel = clean(m[1])
		}
	}
	s.Beschreibung = first(meta["og:description"], meta["description"], meta["twitter:description"])
	s.Site = meta["og:site_name"]
	for _, m := range reLD.FindAllStringSubmatch(page, -1) {
		var v any
		if json.Unmarshal([]byte(strings.TrimSpace(m[1])), &v) != nil {
			continue
		}
		for _, o := range objects(v) {
			s.take(o)
		}
	}
	s.Text = readable(page)
	return s
}

// objects is every JSON-LD object in v: arrays and @graph unfolded.
func objects(v any) []map[string]any {
	var out []map[string]any
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			out = append(out, objects(e)...)
		}
	case map[string]any:
		out = append(out, x)
		if g, ok := x["@graph"]; ok {
			out = append(out, objects(g)...)
		}
	}
	return out
}

// types are an object's @type values.
func types(o map[string]any) map[string]bool {
	out := map[string]bool{}
	switch t := o["@type"].(type) {
	case string:
		out[t] = true
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

// take reads one JSON-LD object into the page, the first of a kind kept.
func (s *Seite) take(o map[string]any) {
	t := types(o)
	switch {
	case t["Recipe"] && s.Rezept == nil:
		r := &Rezept{Name: text(o["name"]), Beschreibung: text(o["description"]), Portionen: yield(o["recipeYield"])}
		for _, z := range list(o["recipeIngredient"]) {
			if z = text(z); z != "" {
				r.Zutaten = append(r.Zutaten, z.(string))
			}
		}
		r.Schritte = steps(o["recipeInstructions"])
		r.Zeit = first(duration(text(o["totalTime"])), sum(duration(text(o["prepTime"])), duration(text(o["cookTime"]))))
		if len(r.Zutaten) > 0 || len(r.Schritte) > 0 {
			s.Rezept = r
		}
	case t["Review"] && s.Album == nil:
		item, _ := o["itemReviewed"].(map[string]any)
		if item == nil || !(types(item)["MusicAlbum"] || types(item)["MusicRecording"] || types(item)["MusicGroup"]) {
			return
		}
		a := album(item)
		a.Rezension = text(o["reviewBody"])
		if rr, ok := o["reviewRating"].(map[string]any); ok {
			if v := text(rr["ratingValue"]); v != "" {
				a.Wertung = v
				if best := text(rr["bestRating"]); best != "" {
					a.Wertung += "/" + best
				}
			}
		}
		s.Album = &a
	case t["MusicAlbum"] && s.Album == nil:
		a := album(o)
		s.Album = &a
	case (t["Hotel"] || t["LodgingBusiness"] || t["Resort"] || t["TouristTrip"] || t["TouristDestination"] || t["VacationRental"]) && s.Reise == nil:
		r := &Reise{Name: text(o["name"]), Beschreibung: text(o["description"]), Ort: place(o["address"]), Preis: price(o["offers"])}
		if r.Preis == "" {
			r.Preis = text(o["priceRange"])
		}
		if p, ok := o["provider"].(map[string]any); ok {
			r.Anbieter = text(p["name"])
		}
		s.Reise = r
	}
}

func album(o map[string]any) Album {
	a := Album{Name: text(o["name"]), Jahr: year(text(first(text(o["datePublished"]), text(o["dateCreated"]))))}
	switch b := o["byArtist"].(type) {
	case map[string]any:
		a.Band = text(b["name"])
	case []any:
		if len(b) > 0 {
			if m, ok := b[0].(map[string]any); ok {
				a.Band = text(m["name"])
			}
		}
	case string:
		a.Band = b
	}
	return a
}

func year(s string) string {
	if len(s) >= 4 {
		if _, err := strconv.Atoi(s[:4]); err == nil {
			return s[:4]
		}
	}
	return ""
}

// text is a JSON-LD value as plain text: a string, a number, a name's
// object, its HTML tags and entities gone.
func text(v any) string {
	switch x := v.(type) {
	case string:
		return clean(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case map[string]any:
		return first(text(x["name"]), text(x["text"]), text(x["@value"]))
	case []any:
		if len(x) > 0 {
			return text(x[0])
		}
	}
	return ""
}

func list(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case nil:
		return nil
	default:
		return []any{x}
	}
}

// steps are a recipe's instructions: a text, a list of texts, HowToSteps,
// HowToSections of steps.
func steps(v any) []string {
	var out []string
	for _, e := range list(v) {
		switch x := e.(type) {
		case string:
			for _, l := range strings.Split(clean(x), "\n") {
				if l = strings.TrimSpace(l); l != "" {
					out = append(out, l)
				}
			}
		case map[string]any:
			if types(x)["HowToSection"] {
				out = append(out, steps(x["itemListElement"])...)
			} else if t := first(parts(x["text"]), text(x["name"])); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// parts is a step's text, which some sites (einfachkochen.de) hand over as
// a list of pieces cut at the commas: "Kürbis waschen", "entstielen und
// halbieren. …". The pieces are one step again, a comma put back where
// the piece before did not end a sentence; text() alone kept the first
// piece and the owner's pumpkin soup lost most of its preparation.
func parts(v any) string {
	l, ok := v.([]any)
	if !ok {
		return text(v)
	}
	var b strings.Builder
	for _, e := range l {
		t := text(e)
		if t == "" {
			continue
		}
		if prev := strings.TrimSpace(b.String()); prev != "" {
			if strings.LastIndexAny(prev, ".!?:;") == len(prev)-1 {
				b.WriteString(" ")
			} else {
				b.WriteString(", ")
			}
		}
		b.WriteString(t)
	}
	return b.String()
}

func yield(v any) string {
	y := text(v)
	if y == "" {
		return ""
	}
	if _, err := strconv.Atoi(y); err == nil {
		return y + " Portionen"
	}
	return y
}

// duration is an ISO 8601 duration in words: PT1H30M → „1 Std. 30 Min.“.
func duration(s string) string {
	m := reISO.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil {
		return ""
	}
	d, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	mi, _ := strconv.Atoi(m[3])
	h += d * 24
	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%d Std.", h))
	}
	if mi > 0 {
		parts = append(parts, fmt.Sprintf("%d Min.", mi))
	}
	return strings.Join(parts, " ")
}

func sum(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + " + " + b
}

func place(v any) string {
	switch x := v.(type) {
	case string:
		return clean(x)
	case map[string]any:
		var p []string
		for _, k := range []string{"addressLocality", "addressRegion", "addressCountry"} {
			if t := text(x[k]); t != "" {
				p = append(p, t)
			}
		}
		return strings.Join(p, ", ")
	}
	return ""
}

func price(v any) string {
	for _, e := range list(v) {
		if o, ok := e.(map[string]any); ok {
			p := first(text(o["price"]), text(o["lowPrice"]))
			if p != "" {
				return strings.TrimSpace(p + " " + text(o["priceCurrency"]))
			}
		}
	}
	return ""
}

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range reAttr.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = first(m[3], m[4], m[5])
	}
	return out
}

func first(vs ...string) string {
	for _, v := range vs {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

// clean is markup as plain text on its lines.
func clean(s string) string {
	s = reBlock.ReplaceAllString(s, "\n")
	s = html.UnescapeString(reTag.ReplaceAllString(s, " "))
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(reSpaces.ReplaceAllString(l, " ")); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// readable is the page's text: its <article>, else <main>, else <body>,
// without scripts, menus and forms; paragraphs of some length only.
func readable(page string) string {
	part := page
	for _, tag := range []string{"article", "main", "body"} {
		if i := strings.Index(strings.ToLower(part), "<"+tag); i >= 0 {
			rest := part[i:]
			if j := strings.LastIndex(strings.ToLower(rest), "</"+tag+">"); j > 0 {
				part = rest[:j]
				break
			}
		}
	}
	part = reDrop.ReplaceAllString(part, " ")
	var keep []string
	n := 0
	for _, p := range strings.Split(clean(part), "\n") {
		if len([]rune(p)) < 40 {
			continue
		}
		keep = append(keep, p)
		if n += len(p); n > maxText {
			break
		}
	}
	return strings.Join(keep, "\n\n")
}
