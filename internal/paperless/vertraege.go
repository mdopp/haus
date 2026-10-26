package paperless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Contracts (haus#139, laib#721): Paperless holds each contract's fields —
// Anbieter, Vertragsnummer, Vertragsbeginn, Vertragsende, Kündigungsfrist,
// Nächster Kündigungstermin, Betrag, Zahlungsintervall — that laib's
// paperless-vertraege fills from the text with word-for-word evidence, else
// it tags the document "prüfen". A question about a contract is answered
// from them, always naming the document.

// VertragsArten are the document types and tags that make a document a
// contract (laib's VERTRAG_ARTEN default).
var VertragsArten = []string{"Vertrag", "Versicherung", "Bescheid"}

// Pruefen is the tag laib puts on a contract it is not sure of.
const Pruefen = "prüfen"

// Vertrag is one contract: its document, its kind, its fields by name,
// when the notice must be in, and whether laib was unsure.
type Vertrag struct {
	Doc
	Art          string
	Felder       map[string]string
	KuendigenBis time.Time // zero: not known
	Unsicher     bool
}

// order is the fields as they are said.
var vertragFelder = []string{"Anbieter", "Vertragsnummer", "Vertragsbeginn", "Vertragsende", "Kündigungsfrist", "Nächster Kündigungstermin", "Betrag", "Zahlungsintervall"}

type named struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"data_type"`
}

func (c *Client) all(ctx context.Context, path string) ([]named, error) {
	var page struct {
		Results []named `json:"results"`
	}
	err := c.get(ctx, path, url.Values{"page_size": {"1000"}}, &page)
	return page.Results, err
}

// Vertraege are the household's contracts that the question names (its
// words in title, Anbieter, correspondent, kind or tags; "Kfz" finds
// "Kfz-Versicherung"), the best first, at most max; with no word that
// names one, none.
func (c *Client) Vertraege(ctx context.Context, frage string, arten []string, max int) ([]Vertrag, error) {
	if len(arten) == 0 {
		arten = VertragsArten
	}
	want := map[string]bool{}
	for _, a := range arten {
		want[strings.ToLower(strings.TrimSpace(a))] = true
	}
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
	typeName, tagName, field := map[int]string{}, map[int]string{}, map[int]named{}
	var typeIDs, tagIDs []int
	for _, t := range types {
		typeName[t.ID] = t.Name
		if want[strings.ToLower(t.Name)] {
			typeIDs = append(typeIDs, t.ID)
		}
	}
	for _, t := range tags {
		tagName[t.ID] = t.Name
		if want[strings.ToLower(t.Name)] {
			tagIDs = append(tagIDs, t.ID)
		}
	}
	for _, f := range fields {
		field[f.ID] = f
	}
	type raw struct {
		ID            int    `json:"id"`
		Title         string `json:"title"`
		Correspondent *int   `json:"correspondent"`
		DocumentType  *int   `json:"document_type"`
		Tags          []int  `json:"tags"`
		Created       string `json:"created"`
		Custom        []struct {
			Field int `json:"field"`
			Value any `json:"value"`
		} `json:"custom_fields"`
	}
	seen := map[int]bool{}
	var docs []raw
	for _, q := range []url.Values{{"document_type__id__in": {idList(typeIDs)}}, {"tags__id__in": {idList(tagIDs)}}} {
		for k, v := range q {
			if v[0] == "" {
				delete(q, k)
			}
		}
		if len(q) == 0 {
			continue
		}
		q.Set("page_size", "200")
		q.Set("fields", "id,title,correspondent,document_type,tags,created,custom_fields")
		var page struct {
			Results []raw `json:"results"`
		}
		if err := c.get(ctx, "/api/documents/", q, &page); err != nil {
			return nil, err
		}
		for _, d := range page.Results {
			if !seen[d.ID] {
				seen[d.ID] = true
				docs = append(docs, d)
			}
		}
	}
	corrIDs := map[int]bool{}
	for _, d := range docs {
		if d.Correspondent != nil {
			corrIDs[*d.Correspondent] = true
		}
	}
	corr := c.names(ctx, "/api/correspondents/", corrIDs)
	words := frageWorte(frage)
	type scored struct {
		v     Vertrag
		score int
	}
	var hits []scored
	for _, d := range docs {
		v := Vertrag{Doc: Doc{ID: d.ID, Title: d.Title, Link: c.Page(d.ID)}, Felder: map[string]string{}}
		if d.Correspondent != nil {
			v.Correspondent = corr[*d.Correspondent]
		}
		if d.DocumentType != nil {
			v.Art = typeName[*d.DocumentType]
		}
		for _, t := range d.Tags {
			v.Tags = append(v.Tags, tagName[t])
			if strings.EqualFold(tagName[t], Pruefen) {
				v.Unsicher = true
			}
			if v.Art == "" && want[strings.ToLower(tagName[t])] {
				v.Art = tagName[t]
			}
		}
		if len(d.Created) >= 10 {
			v.Created, _ = time.Parse("2006-01-02", d.Created[:10])
		}
		for _, cf := range d.Custom {
			f, ok := field[cf.Field]
			if !ok || cf.Value == nil {
				continue
			}
			if s := feldWert(f.DataType, cf.Value); s != "" {
				v.Felder[f.Name] = s
			}
		}
		v.KuendigenBis = kuendigenBis(v.Felder["Nächster Kündigungstermin"], v.Felder["Kündigungsfrist"])
		hay := plainWords(strings.Join(append([]string{v.Title, v.Felder["Anbieter"], v.Correspondent, v.Art}, v.Tags...), " "))
		score := 0
		for _, w := range words {
			if strings.Contains(hay, w) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{v, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].v.Created.After(hits[j].v.Created)
	})
	var out []Vertrag
	for i := 0; i < len(hits) && i < max && hits[i].score == hits[0].score; i++ { // the best only: "Kfz-Versicherung" is not every Versicherung
		out = append(out, hits[i].v)
	}
	return out, nil
}

// frageNicht are a question's words that name no contract.
var frageNicht = map[string]bool{"wann": true, "läuft": true, "laeuft": true, "aus": true, "bis": true, "muss": true, "ich": true, "wir": true, "den": true,
	"die": true, "der": true, "das": true, "dem": true, "mein": true, "meine": true, "meinen": true, "unser": true, "unsere": true, "unseren": true,
	"kündigen": true, "kuendigen": true, "kündigung": true, "vertrag": true, "vertrags": true, "wie": true, "viel": true, "kostet": true, "zahlen": true,
	"ist": true, "sind": true, "hat": true, "haben": true, "für": true, "fuer": true, "mit": true, "von": true, "bei": true, "und": true, "oder": true,
	"was": true, "welche": true, "welcher": true, "frist": true, "ende": true, "endet": true, "noch": true, "spätestens": true, "nächste": true}

// frageWorte are the words of a question that may name a contract: three
// letters and more, no question words.
func frageWorte(q string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(q), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(w)) >= 3 && !frageNicht[w] {
			out = append(out, w)
		}
	}
	return out
}

func plainWords(s string) string { return " " + strings.ToLower(s) + " " }

// feldWert is a custom field's value as it is said: a date as TT.MM.JJJJ,
// an amount as "84,50 €".
func feldWert(kind string, v any) string {
	s := strings.TrimSpace(fmt.Sprint(v))
	switch kind {
	case "date":
		if t, err := time.Parse("2006-01-02", s); err == nil {
			return t.Format("02.01.2006")
		}
	case "monetary":
		n := strings.TrimLeft(s, "ABCDEFGHIJKLMNOPQRSTUVWXYZ")
		if f, err := strconv.ParseFloat(n, 64); err == nil {
			return strings.Replace(strconv.FormatFloat(f, 'f', 2, 64), ".", ",", 1) + " €"
		}
	}
	return s
}

var (
	reFrist = regexp.MustCompile(`(?i)(\d{1,3})\s*(tag|woche|monat|jahr)`)
	reEnde  = regexp.MustCompile(`(?i)zum\s+(monats|quartals|halbjahres|jahres)ende`)
)

// kuendigenBis is when the notice must be in, as laib reckons it
// (paperless-vertraege.py minus): the notice date less the period; "zum
// Monats-/Quartalsende" with a notice date at such an end reaches back to
// a month's end. Zero when either is missing or unreadable.
func kuendigenBis(termin, frist string) time.Time {
	d, err := time.Parse("02.01.2006", termin)
	if err != nil {
		return time.Time{}
	}
	m := reFrist.FindStringSubmatch(frist)
	if m == nil {
		return time.Time{}
	}
	n, _ := strconv.Atoi(m[1])
	unit := strings.ToLower(m[2])
	ende := 0
	if e := reEnde.FindStringSubmatch(frist); e != nil {
		ende = map[string]int{"monats": 1, "quartals": 3, "halbjahres": 6, "jahres": 12}[strings.ToLower(e[1])]
	}
	switch unit {
	case "monat", "jahr":
		k := n
		if unit == "jahr" {
			k *= 12
		}
		lastOfMonth := d.AddDate(0, 0, 1).Day() == 1
		if ende > 0 && lastOfMonth && int(d.Month())%ende == 0 { // the month's end k months back
			return time.Date(d.Year(), d.Month()-time.Month(k)+1, 0, 0, 0, 0, 0, time.UTC)
		}
		y, mo := d.Year(), int(d.Month())-k
		for mo <= 0 {
			mo += 12
			y--
		}
		last := time.Date(y, time.Month(mo)+1, 0, 0, 0, 0, 0, time.UTC).Day()
		return time.Date(y, time.Month(mo), min(d.Day(), last), 0, 0, 0, 0, time.UTC)
	case "woche":
		return d.AddDate(0, 0, -7*n)
	}
	return d.AddDate(0, 0, -n)
}

// ErrNoChat is a Paperless without its document chat (before 3.0, or the
// AI off).
var ErrNoChat = errors.New("Paperless: kein Dokument-Chat")

// Chat asks Paperless's document chat (3.0, the household model through
// laib's proxy) about one document; its answer as text.
func (c *Client) Chat(ctx context.Context, id int, frage string) (string, error) {
	body, _ := json.Marshal(map[string]any{"q": frage, "document_id": id})
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/api/documents/chat/", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Token "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	h := defaultHTTP
	if c.HTTP != nil {
		hc := *c.HTTP
		hc.CheckRedirect = noRedirect
		h = &hc
	}
	resp, err := h.Do(req)
	if err != nil {
		return "", c.scrub(fmt.Errorf("Paperless nicht erreichbar: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
		return "", ErrNoChat
	}
	if resp.StatusCode != http.StatusOK {
		return "", &ErrStatus{resp.StatusCode}
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
	return strings.TrimSpace(string(b)), nil
}
