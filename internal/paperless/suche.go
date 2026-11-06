package paperless

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// The household's order in Paperless (dokumente's ordnen, 2026-11-05) as
// Homes' Dokumente page searches it (owner 2026-11-06: „Kategorien und
// Rechnungsnummern/Versicherungsart+Nr/gekaufte Produkte … von der
// Oberfläche suchbar“): Bereich tags, document types, the Person field
// and the number fields. Checked on Paperless 3.0.0 (the box,
// 2026-11-06): `text` is its own search box's mode (title and text,
// substring; `query` answers 400 to a typed "/" or ":"), and
// custom_field_query takes a field by id, ["OR"|"AND", [...]],
// icontains on string and longtext, gt on a date, exact on a select's
// option id; a field it lacks is a 400, so only fields it has are asked.

// Bereiche are the Bereich tags, in ordnen's order.
var Bereiche = []string{"Haus", "Auto", "Gesundheit", "Versicherung", "Finanzen", "Steuer", "Arbeit & Rente", "Freizeit"}

// Nummern are the fields one search field searches beside title, text and
// sender: a number typed, or a product bought.
var Nummern = []string{"Rechnungsnummer", "Vertragsnummer", "Versicherungsnummer", "Kundennummer", "Produkte", "Anbieter"}

// Gezaehlt is a Bereich or a type with how many documents it has.
type Gezaehlt struct {
	Name   string
	Anzahl int
}

// Ordnung is what Paperless has of the order: the Bereiche it has (in
// Bereiche's order), the types with a document (the most first), the
// Person field's options.
type Ordnung struct {
	Bereiche, Typen []Gezaehlt
	Personen        []string
	typ, tag        map[string]int // by folded name
	feld            map[string]named
	feldID          map[int]named
	person          map[string]string // label → option id
	typName, tagNam map[int]string
}

func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// ids are the ids of these names in m; a name m lacks is left out.
func (o *Ordnung) ids(m map[string]int, names []string) []int {
	var out []int
	for _, n := range names {
		if id, ok := m[fold(n)]; ok {
			out = append(out, id)
		}
	}
	return out
}

// ordnung asks Paperless for its types, tags and fields (three requests).
func (c *Client) ordnung(ctx context.Context) (*Ordnung, error) {
	types, err := c.all(ctx, "/api/document_types/")
	if err != nil {
		return nil, err
	}
	tags, err := c.all(ctx, "/api/tags/")
	if err != nil {
		return nil, err
	}
	fields, err := c.all(ctx, "/api/custom_fields/")
	if err != nil {
		return nil, err
	}
	o := &Ordnung{typ: map[string]int{}, tag: map[string]int{}, feld: map[string]named{}, feldID: map[int]named{},
		person: map[string]string{}, typName: map[int]string{}, tagNam: map[int]string{}}
	for _, t := range types {
		o.typ[fold(t.Name)], o.typName[t.ID] = t.ID, t.Name
		if t.Count > 0 {
			o.Typen = append(o.Typen, Gezaehlt{t.Name, t.Count})
		}
	}
	slices.SortFunc(o.Typen, func(a, b Gezaehlt) int { return cmp.Or(b.Anzahl-a.Anzahl, cmp.Compare(a.Name, b.Name)) })
	count := map[string]Gezaehlt{}
	for _, t := range tags {
		o.tag[fold(t.Name)], o.tagNam[t.ID] = t.ID, t.Name
		count[fold(t.Name)] = Gezaehlt{t.Name, t.Count}
	}
	for _, b := range Bereiche {
		if g, ok := count[fold(b)]; ok && g.Anzahl > 0 {
			o.Bereiche = append(o.Bereiche, Gezaehlt{b, g.Anzahl}) // ordnen's spelling: gesundheit is Gesundheit
		}
	}
	for _, f := range fields {
		o.feld[f.Name], o.feldID[f.ID] = f, f
	}
	for _, p := range o.feld["Person"].Extra.Options {
		if p.Label != "" {
			o.Personen, o.person[p.Label] = append(o.Personen, p.Label), p.ID
		}
	}
	return o, nil
}

// merkZeit is how long an answer is kept: the page is fetched again while
// typing and on every tap, Paperless need not be asked each time.
var merkZeit = time.Minute

var merk = struct {
	sync.Mutex
	m map[string]gemerkt
}{m: map[string]gemerkt{}}

type gemerkt struct {
	bis time.Time
	v   any
}

// gemerkt is f's answer, kept merkZeit under key (with the client's
// address in it, so two clients never share one); an error is not kept.
func merken[T any](c *Client, key string, f func() (T, error)) (T, error) {
	key = fmt.Sprintf("%p %s", c, key)
	merk.Lock()
	if g, ok := merk.m[key]; ok && time.Now().Before(g.bis) {
		merk.Unlock()
		return g.v.(T), nil
	}
	merk.Unlock()
	v, err := f()
	if err != nil || merkZeit <= 0 {
		return v, err
	}
	merk.Lock()
	defer merk.Unlock()
	if len(merk.m) > 500 {
		clear(merk.m)
	}
	merk.m[key] = gemerkt{time.Now().Add(merkZeit), v}
	return v, nil
}

// Ordnung is the order as Paperless has it now, kept a minute.
func (c *Client) Ordnung(ctx context.Context) (*Ordnung, error) {
	return merken(c, "ordnung", func() (*Ordnung, error) { return c.ordnung(ctx) })
}

// eintraege reads the documents q finds with their kind and fields, at
// most max.
func (c *Client) eintraege(ctx context.Context, o *Ordnung, q url.Values, max int) ([]Eintrag, error) {
	var page struct {
		Results []struct {
			apiDoc
			DocumentType *int `json:"document_type"`
			Custom       []struct {
				Field int `json:"field"`
				Value any `json:"value"`
			} `json:"custom_fields"`
		} `json:"results"`
	}
	q.Set("page_size", strconv.Itoa(max))
	q.Set("fields", "id,title,correspondent,document_type,tags,created,custom_fields")
	if err := c.get(ctx, "/api/documents/", q, &page); err != nil {
		return nil, err
	}
	raw := make([]apiDoc, len(page.Results))
	for i, d := range page.Results {
		raw[i] = d.apiDoc
		raw[i].Tags = nil // named from the order, not asked again
	}
	docs := c.named(ctx, raw)
	out := make([]Eintrag, 0, len(docs))
	for i, d := range page.Results {
		e := Eintrag{Doc: docs[i], Felder: map[string]string{}}
		if d.DocumentType != nil {
			e.Art = o.typName[*d.DocumentType]
		}
		for _, t := range d.Tags {
			e.Tags = append(e.Tags, o.tagNam[t])
			e.Unsicher = e.Unsicher || strings.EqualFold(o.tagNam[t], Pruefen)
		}
		for _, cf := range d.Custom {
			if f, ok := o.feldID[cf.Field]; ok && cf.Value != nil {
				if s := feldWert(f.DataType, cf.Value); s != "" {
					e.Felder[f.Name] = s
				}
			}
		}
		e.KuendigenBis = kuendigenBis(e.Felder["Nächster Kündigungstermin"], e.Felder["Kündigungsfrist"])
		out = append(out, e)
	}
	return out, nil
}

// Filter is what the family asks for; every part set narrows.
type Filter struct {
	Text       string    // title, text, sender, the Nummern
	Bereich    string    // a Bereich tag
	Typen      []string  // document types, any of them
	Person     string    // an option of the Person field
	GarantieAb time.Time // a „Garantie bis“ after this day
	Max        int
}

// Finde lists the documents f asks for, at most f.Max: without words the
// newest first; with words the number fields' hits, then the senders',
// then the text's, each document once. A filter naming what Paperless
// lacks finds nothing. Kept a minute.
func (c *Client) Finde(ctx context.Context, f Filter) ([]Eintrag, error) {
	o, err := c.Ordnung(ctx)
	if err != nil {
		return nil, err
	}
	base, atoms, ok := o.filter(f)
	if !ok {
		return nil, nil
	}
	with := func(more ...any) url.Values {
		q := url.Values{}
		for k, v := range base {
			q[k] = v
		}
		if a := append(slices.Clone(atoms), more...); len(a) > 0 {
			b, _ := json.Marshal([]any{"AND", a})
			q.Set("custom_field_query", string(b))
		}
		return q
	}
	text := strings.Join(strings.Fields(f.Text), " ")
	if text == "" {
		q := with()
		q.Set("ordering", "-created")
		return merken(c, q.Encode(), func() ([]Eintrag, error) { return c.eintraege(ctx, o, q, f.Max) })
	}
	type ask struct {
		q    url.Values
		max  int
		keep func(Eintrag) bool
	}
	var asks []ask
	if nr := o.nummern("icontains", text); len(nr) > 0 {
		asks = append(asks, ask{with([]any{"OR", nr}), f.Max, nil})
	}
	if want := dicht(text); strings.ContainsAny(want, "0123456789") {
		// a number typed without its spaces or dashes, or with others: every
		// document with a number field, compared without them (a household's
		// few hundred, kept a minute whatever is typed)
		asks = append(asks, ask{with([]any{"OR", o.nummern("exists", true)}), 500, func(e Eintrag) bool {
			for _, n := range Nummern {
				if v := dicht(e.Felder[n]); v != "" && strings.Contains(v, want) {
					return true
				}
			}
			return false
		}})
	}
	for _, k := range []string{"correspondent__name__icontains", "text"} {
		q := with()
		q.Set(k, text)
		asks = append(asks, ask{q, f.Max, nil})
	}
	found := make([][]Eintrag, len(asks))
	errs := make([]error, len(asks))
	var wg sync.WaitGroup
	for i, a := range asks {
		wg.Go(func() {
			found[i], errs[i] = merken(c, a.q.Encode(), func() ([]Eintrag, error) { return c.eintraege(ctx, o, a.q, a.max) })
		})
	}
	wg.Wait()
	var out []Eintrag
	seen := map[int]bool{}
	failed := 0
	for i, a := range asks {
		if errs[i] != nil {
			failed++
			err = errs[i]
		}
		for _, e := range found[i] {
			if !seen[e.ID] && len(out) < f.Max && (a.keep == nil || a.keep(e)) {
				seen[e.ID] = true
				out = append(out, e)
			}
		}
	}
	if failed < len(asks) {
		err = nil // one ask answering is a result
	}
	return out, err
}

// filter is f as the list's query and its custom-field conditions; false
// when it names a Bereich, type, person or field Paperless lacks.
func (o *Ordnung) filter(f Filter) (url.Values, []any, bool) {
	q, atoms := url.Values{}, []any{}
	if f.Bereich != "" {
		id, ok := o.tag[fold(f.Bereich)]
		if !ok {
			return nil, nil, false
		}
		q.Set("tags__id__all", strconv.Itoa(id))
	}
	if len(f.Typen) > 0 {
		ids := o.ids(o.typ, f.Typen)
		if len(ids) == 0 {
			return nil, nil, false
		}
		q.Set("document_type__id__in", idList(ids))
	}
	if f.Person != "" {
		opt, ok := o.person[f.Person]
		if !ok {
			return nil, nil, false
		}
		atoms = append(atoms, []any{o.feld["Person"].ID, "exact", opt})
	}
	if !f.GarantieAb.IsZero() {
		g, ok := o.feld["Garantie bis"]
		if !ok {
			return nil, nil, false
		}
		atoms = append(atoms, []any{g.ID, "gt", f.GarantieAb.Format("2006-01-02")})
	}
	return q, atoms, true
}

// nummern are the conditions [field, op, value] on each string or
// longtext field of Nummern Paperless has.
func (o *Ordnung) nummern(op string, value any) []any {
	var out []any
	for _, n := range Nummern {
		if f, ok := o.feld[n]; ok && (f.DataType == "string" || f.DataType == "longtext") {
			out = append(out, []any{f.ID, op, value})
		}
	}
	return out
}

// dicht is s as a number is compared: lower case, letters and digits only
// ("KV 123-456" is "kv123456").
func dicht(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, s)
}
