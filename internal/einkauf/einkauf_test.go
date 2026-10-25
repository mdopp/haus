package einkauf

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/caldav/davfake"
	"github.com/mdopp/haus/internal/jellyfin"
	"github.com/mdopp/haus/internal/merken"
	"github.com/mdopp/haus/internal/musicbrainz"
	"github.com/mdopp/haus/internal/notes"
)

func TestParse(t *testing.T) {
	for in, want := range map[string]Posten{
		"200 g Mehl":                 {200, "g", "Mehl", ""},
		"200g Mehl":                  {200, "g", "Mehl", ""},
		"1,5 kg Kartoffeln":          {1500, "g", "Kartoffeln", ""},
		"0.5 l Milch":                {500, "ml", "Milch", ""},
		"1/2 TL Salz":                {.5, "TL", "Salz", ""},
		"1 1/2 EL Zucker":            {1.5, "EL", "Zucker", ""},
		"½ Zitrone":                  {.5, "", "Zitrone", ""},
		"1½ Tassen Reis":             {1.5, "Tasse", "Reis", ""},
		"1 Prise Salz":               {1, "Prise", "Salz", ""},
		"2-3 Zwiebeln, fein gehackt": {3, "", "Zwiebeln", "fein gehackt"},
		"3 Eier (Größe M)":           {3, "", "Eier", "Größe M"},
		"2 Stück Paprika":            {2, "", "Paprika", ""},
		"1 Dose Tomaten":             {1, "Dose", "Tomaten", ""},
		"2 Zehen Knoblauch":          {2, "Zehe", "Knoblauch", ""},
		"ca. 100 ml Sahne":           {100, "ml", "Sahne", ""},
		"Milch":                      {0, "", "Milch", ""},
		"etwas Pfeffer":              {0, "", "Pfeffer", "etwas"},
		"- 250 g Butter":             {250, "g", "Butter", ""},
		"Salz und Pfeffer":           {0, "", "Salz und Pfeffer", ""},
	} {
		if got := Parse(in); got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestStringReadsBack(t *testing.T) {
	for in, want := range map[string]string{
		"200 g Mehl": "200 g Mehl", "1500 g Kartoffeln": "1,5 kg Kartoffeln", "1000 ml Milch": "1 l Milch",
		"1/2 TL Salz": "½ TL Salz", "2 Prisen Salz": "2 Prisen Salz", "2 Dosen Tomaten": "2 Dosen Tomaten",
		"1 Dose Tomaten": "1 Dose Tomaten", "3 Eier": "3 Eier", "Milch": "Milch", "etwas Pfeffer": "etwas Pfeffer",
		"2 Zwiebeln, gehackt": "2 Zwiebeln, gehackt", "1,25 l Wasser": "1,25 l Wasser",
	} {
		got := Parse(in).String()
		if got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
		if again := Parse(got).String(); again != got { // a line read back from the list is the same line
			t.Errorf("%q read back → %q", got, again)
		}
	}
}

func TestAdd(t *testing.T) {
	sum := func(a, b string) string {
		p, ok := Add(Parse(a), Parse(b))
		if !ok {
			return "—"
		}
		return p.String()
	}
	for _, c := range [][3]string{
		{"200 g Mehl", "300 g Mehl", "500 g Mehl"},
		{"800 g Mehl", "0,5 kg Mehl", "1,3 kg Mehl"},
		{"250 ml Milch", "1 l Milch", "1,25 l Milch"},
		{"1 Ei", "2 Eier", "3 Eier"},
		{"1 Zwiebel", "2 Zwiebeln", "3 Zwiebeln"},
		{"1 TL Salz", "1 Prise Salz", "—"}, // different units: side by side
		{"200 g Mehl", "1 Tasse Mehl", "—"},
		{"Milch", "1 l Milch", "1 l Milch"}, // no amount goes into one with
		{"1 l Milch", "Milch", "1 l Milch"},
		{"Milch", "Milch", "Milch"},
		{"Milch", "Mehl", "—"},
		{"1 Dose Tomaten", "2 Dosen Tomaten", "3 Dosen Tomaten"},
		{"2 Zwiebeln, gehackt", "1 Zwiebel, in Ringen", "3 Zwiebeln"},
	} {
		if got := sum(c[0], c[1]); got != c[2] {
			t.Errorf("%q + %q = %q, want %q", c[0], c[1], got, c[2])
		}
	}
}

func TestMal(t *testing.T) {
	for _, c := range []struct {
		in   string
		f    float64
		want string
	}{
		{"200 g Mehl", 2, "400 g Mehl"},
		{"250 g Butter", 1.5, "375 g Butter"},
		{"125 g Quark", 4.0 / 3, "165 g Quark"}, // to 5 g
		{"1 Ei", 1.5, "1½ Ei"},                  // a count up to a half
		{"3 Eier", 0.5, "1½ Eier"},
		{"1 Dose Tomaten", 1.5, "2 Dosen Tomaten"}, // bought whole: up
		{"1 TL Salz", 0.5, "½ TL Salz"},
		{"1 Prise Salz", 0.25, "½ Prise Salz"},
		{"Milch", 3, "Milch"},
		{"750 ml Brühe", 2, "1,5 l Brühe"},
	} {
		if got := Parse(c.in).Mal(c.f).String(); got != c.want {
			t.Errorf("%q × %v = %q, want %q", c.in, c.f, got, c.want)
		}
	}
}

func TestAusNotizAndFuer(t *testing.T) {
	lasagne := AusNotiz(notes.Note{Name: "lasagne.md", Title: "Lasagne", Text: "---\nart: rezept\n---\n# Lasagne\n\n*Für 4 Portionen · 1 Std.*\n\n## Zutaten\n\n- 500 g Hackfleisch\n- 2 Zwiebeln\n- Für die Soße:\n- 1 Dose Tomaten\n- 200 g Mehl\n\n## Zubereitung\n\n1. Kochen.\n- kein Zutat\n"})
	if lasagne.Portionen != 4 || len(lasagne.Zutaten) != 4 || lasagne.Zutaten[2] != "1 Dose Tomaten" {
		t.Fatalf("lasagne = %+v", lasagne)
	}
	curry := AusNotiz(notes.Note{Name: "curry.md", Title: "Curry", Text: "# Curry\n\n*Für 2 Portionen*\n\n## Zutaten\n\n- 1 Zwiebel\n- 100 g Mehl\n- Salz\n"})
	got := Fuer([]Rezept{lasagne, curry}, 4)
	var lines []string
	for _, z := range got {
		lines = append(lines, z.Text()+" ["+herkunft(z.Herkunft)+"]")
	}
	want := []string{"500 g Hackfleisch [Lasagne]", "4 Zwiebeln [Lasagne, Curry]", "1 Dose Tomaten [Lasagne]", "400 g Mehl [Lasagne, Curry]", "Salz [Curry]"}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("Fuer =\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if as := Fuer([]Rezept{curry}, 0); as[1].Text() != "100 g Mehl" {
		t.Errorf("as written: %+v", as)
	}
}

func list(t *testing.T) (*Liste, *davfake.Server, *int) {
	t.Helper()
	dav := davfake.New("haus", "pw")
	t.Cleanup(dav.Close)
	l := Open(&caldav.Client{URL: dav.URL, User: "haus", Password: "pw"})
	told := 0
	l.Changed = func(caldav.ListRef) { told++ }
	if err := l.Ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	return l, dav, &told
}

func lines(t *testing.T, l *Liste) []string {
	t.Helper()
	items, err := l.Lesen(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, it := range items {
		s := it.Summary
		if it.Herkunft != "" {
			s += " [" + it.Herkunft + "]"
		}
		if it.Done {
			s = "✓ " + s
		}
		out = append(out, s)
	}
	return out
}

func TestListeAddsUpTicksAndTidies(t *testing.T) {
	ctx := context.Background()
	l, dav, told := list(t)
	if got := dav.Calendars(); len(got) != 1 || got[0] != "/haus/einkauf/" || dav.DisplayName("/haus/einkauf/") != "Einkauf" {
		t.Fatalf("calendars = %v", got)
	}
	if err := l.Ensure(ctx); err != nil { // there already: no error
		t.Fatal(err)
	}
	res, err := l.Dazu(ctx, []Zeile{{Posten: Parse("Milch")}, {Posten: Parse("200 g Mehl"), Herkunft: []string{"Pizza"}}})
	if err != nil || res != (Ergebnis{Neu: 2}) {
		t.Fatalf("Dazu = %+v, %v", res, err)
	}
	res, err = l.Dazu(ctx, []Zeile{{Posten: Parse("300 g Mehl"), Herkunft: []string{"Kuchen"}}, {Posten: Parse("1 l Milch")}, {Posten: Parse("Butter")}})
	if err != nil || res != (Ergebnis{Neu: 1, Addiert: 2}) {
		t.Fatalf("Dazu = %+v, %v", res, err)
	}
	if got := strings.Join(lines(t, l), "|"); got != "1 l Milch|500 g Mehl [Pizza, Kuchen]|Butter" {
		t.Errorf("list = %s", got)
	}
	items, _ := l.Lesen(ctx)
	if err := l.Haken(ctx, items[0].UID, true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lines(t, l), "|"); got != "500 g Mehl [Pizza, Kuchen]|Butter|✓ 1 l Milch" {
		t.Errorf("ticked: %s", got)
	}
	// a ticked-off line is not added to: Milch again is a new line
	if res, _ := l.Dazu(ctx, []Zeile{{Posten: Parse("Milch")}}); res.Neu != 1 {
		t.Errorf("onto a ticked line: %+v", res)
	}
	if err := l.Haken(ctx, items[0].UID, false); err != nil { // back on
		t.Fatal(err)
	}
	if err := l.Haken(ctx, items[0].UID, true); err != nil {
		t.Fatal(err)
	}
	if err := l.Haken(ctx, "nix", true); err != ErrNoLine {
		t.Errorf("unknown line: %v", err)
	}
	n, err := l.Aufraeumen(ctx)
	if err != nil || n != 1 {
		t.Fatalf("Aufraeumen = %d, %v", n, err)
	}
	if got := strings.Join(lines(t, l), "|"); got != "500 g Mehl [Pizza, Kuchen]|Butter|Milch" {
		t.Errorf("tidied: %s", got)
	}
	if *told < 6 {
		t.Errorf("told %d times", *told)
	}
	// the list is no calendar and its lines are no reminders
	c := l.DAV
	if cals, _ := c.Calendars(ctx); len(cals) != 0 {
		t.Errorf("calendars show the list: %+v", cals)
	}
	if todos, _ := c.OpenTodos(ctx); len(todos) != 0 {
		t.Errorf("reminders show the list: %+v", todos)
	}
}

func TestListeReadsAPhonesLine(t *testing.T) {
	ctx := context.Background()
	l, dav, _ := list(t)
	dav.Put("/haus/einkauf/phone.ics", "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VTODO\r\nUID:phone-1\r\nSUMMARY:2 Eier\r\nCATEGORIES:Kühlregal\r\nDTSTAMP:20261025T080000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	if res, err := l.Dazu(ctx, []Zeile{{Posten: Parse("1 Ei"), Herkunft: []string{"Kuchen"}}}); err != nil || res.Addiert != 1 {
		t.Fatalf("Dazu = %+v, %v", res, err)
	}
	ics := dav.Objects()["/haus/einkauf/phone.ics"]
	if !strings.Contains(ics, "SUMMARY:3 Eier") || !strings.Contains(ics, "CATEGORIES:Kühlregal") || !strings.Contains(ics, "DESCRIPTION:Kuchen") {
		t.Errorf("the phone's line, added to:\n%s", ics)
	}
	if err := l.Haken(ctx, "phone-1", true); err != nil {
		t.Fatal(err)
	}
	if ics := dav.Objects()["/haus/einkauf/phone.ics"]; !strings.Contains(ics, "STATUS:COMPLETED") || strings.Count(ics, "SUMMARY:") != 1 {
		t.Errorf("ticked:\n%s", ics)
	}
}

func TestListeRetriesWhenAPhoneChangedTheLine(t *testing.T) {
	ctx := context.Background()
	l, dav, _ := list(t)
	if _, err := l.Dazu(ctx, []Zeile{{Posten: Parse("200 g Mehl")}}); err != nil {
		t.Fatal(err)
	}
	var path string
	for p := range dav.Objects() {
		path = p
	}
	dav.RaceOnPut = path
	if res, err := l.Dazu(ctx, []Zeile{{Posten: Parse("100 g Mehl")}}); err != nil || res.Addiert != 1 {
		t.Fatalf("Dazu after a race = %+v, %v", res, err)
	}
	if got := strings.Join(lines(t, l), "|"); got != "300 g Mehl" {
		t.Errorf("list = %s", got)
	}
}

// haus#119: a list by how someone names it — the household's by default,
// one's own as "meine" (made on first use), a named one by its name; a
// person's own list is no one else's.
func TestWaehle(t *testing.T) {
	ctx := context.Background()
	l, dav, _ := list(t)
	dav.AddPrincipal("anna")
	dav.AddPrincipal("ben")
	if _, err := l.DAV.MakeList(ctx, "", "baumarkt", "Baumarkt"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "Einkauf", "einkaufsliste", "die Einkaufsliste"} {
		if got, _, err := l.Waehle(ctx, "anna", name); err != nil || got.Ref != caldav.Einkauf {
			t.Errorf("%q → %+v %v", name, got, err)
		}
	}
	if got, _, err := l.Waehle(ctx, "anna", "baumarkt"); err != nil || got.Ref.ID() != "liste-baumarkt" {
		t.Errorf("baumarkt → %+v %v", got, err)
	}
	got, made, err := l.Waehle(ctx, "anna", "meine")
	if err != nil || !made || got.Ref.ID() != "anna/liste-meine-liste" {
		t.Fatalf("meine → %+v %v %v", got, made, err)
	}
	if again, made, _ := l.Waehle(ctx, "anna", "Meine Liste"); made || again.Ref != got.Ref {
		t.Errorf("again → %+v %v", again, made)
	}
	if _, err := l.Andere(got.Ref).Dazu(ctx, []Zeile{{Posten: Parse("Socken")}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Waehle(ctx, "ben", "Meine Liste"); err != nil { // Ben's own, not Anna's
		t.Fatal(err)
	}
	if ls, _ := l.DAV.Lists(ctx, "ben"); len(ls) != 3 || ls[2].Ref.Person != "ben" {
		t.Errorf("Ben's lists: %+v", ls)
	}
	if _, _, err := l.Waehle(ctx, "anna", "Urlaub"); err == nil || !strings.Contains(err.Error(), "Keine Liste heißt „Urlaub“. Es gibt: Einkauf, Baumarkt, Meine Liste.") {
		t.Errorf("unknown: %v", err)
	}
	l.DAV.MakeList(ctx, "anna", "geschenke", "Geschenke")
	if _, _, err := l.Waehle(ctx, "anna", "meine"); err == nil || !strings.Contains(err.Error(), "mehrere eigene Listen") {
		t.Errorf("two own: %v", err)
	}
	if _, _, err := l.Waehle(ctx, "", "meine"); err == nil {
		t.Error("meine without a person")
	}
	for id, ok := range map[string]bool{"einkauf": true, "liste-x": true, "anna/liste-x": true, "anna/einkauf": false, "haushalt": false, "anna/../liste-x": false, "a/b/liste-x": false, "": false} {
		if _, got := caldav.ParseListRef(id); got != ok {
			t.Errorf("ParseListRef(%q) = %v", id, got)
		}
	}
}

type fakeBib map[string]jellyfin.Treffer // "artist|title"

func (b fakeBib) Find(ctx context.Context, artist, title string) (jellyfin.Treffer, bool, error) {
	t, ok := b[jellyfin.Plain(artist)+"|"+jellyfin.Plain(title)]
	return t, ok, nil
}

// owner 2026-10-25: a shared song goes on the household's list "Musik",
// unless it is in the music; one that arrives is ticked off and the one
// who wished it is told; "2Pac – Changes" stays a song, no amount.
func TestMusikWuensche(t *testing.T) {
	ctx := context.Background()
	l, dav, _ := list(t)
	bib := fakeBib{"wanda|bologna": {Album: "Amore", Titel: "Bologna"}}
	w := &Wuensche{Listen: l, Bib: bib}
	g, err := w.Wuenschen(ctx, merken.Lied{Kuenstler: "Wanda", Titel: "Salbei", Link: "https://youtube.com/watch?v=x"}, "anna")
	if err != nil || g.Schon || !strings.HasPrefix(g.Satz, "Auf der Musik-Wunschliste: „Wanda – Salbei“.") {
		t.Fatalf("%+v %v", g, err)
	}
	if got := dav.DisplayName("/haus/liste-musik/"); got != "Musik" {
		t.Errorf("list %q", got)
	}
	if g, _ := w.Wuenschen(ctx, merken.Lied{Kuenstler: "wanda", Titel: "salbei"}, "ben"); !g.Steht {
		t.Errorf("twice: %+v", g)
	}
	if g, _ := w.Wuenschen(ctx, merken.Lied{Kuenstler: "Wanda", Titel: "Bologna"}, "anna"); !g.Schon || g.Satz != "Hast du schon: „Wanda – Bologna“ auf dem Album „Amore“." {
		t.Errorf("there already: %+v", g)
	}
	w.Wuenschen(ctx, merken.Lied{Kuenstler: "2Pac", Titel: "Changes"}, "")
	items, _ := l.Andere(MusikListe).Lesen(ctx)
	if len(items) != 2 || items[0].Summary != "Wanda – Salbei" || items[0].Herkunft != "von anna, https://youtube.com/watch?v=x" || items[1].Summary != "2Pac – Changes" {
		t.Fatalf("%+v", items)
	}
	if done, _ := w.Pruefen(ctx); len(done) != 0 {
		t.Errorf("nothing arrived yet: %+v", done)
	}
	bib["wanda|salbei"] = jellyfin.Treffer{Album: "Ciao!", Titel: "Salbei"}
	done, err := w.Pruefen(ctx)
	if err != nil || len(done) != 1 || done[0].Person != "anna" || done[0].Satz != "„Wanda – Salbei“ ist jetzt in deiner Musik, auf dem Album „Ciao!“. Ich habe es auf der Wunschliste abgehakt." {
		t.Fatalf("%+v %v", done, err)
	}
	if items, _ := l.Andere(MusikListe).Lesen(ctx); !items[1].Done {
		t.Errorf("not ticked: %+v", items)
	}
	if done, _ := w.Pruefen(ctx); len(done) != 0 {
		t.Errorf("told twice: %+v", done)
	}
	if k := Kaufen("Wanda – Salbei"); k[0][1] != "https://bandcamp.com/search?q=Wanda+Salbei" {
		t.Errorf("%v", k)
	}
	var none *Wuensche
	if _, ok, _ := none.AusLink(ctx, merken.Fetcher{}, "https://youtube.com/watch?v=x", "anna"); ok {
		t.Error("no list, no wish")
	}
}

type fakeSuche struct {
	fund  musicbrainz.Fund
	err   error
	calls chan string
}

func (s *fakeSuche) Suchen(ctx context.Context, artist, title string, album bool, now time.Time) (musicbrainz.Fund, error) {
	if s.calls != nil {
		s.calls <- artist + " – " + title
	}
	return s.fund, s.err
}

// owner 2026-10-25: a wish moves on — ordered as a record with its
// download or as the file, the file there, the record there — and shows
// where it is to be had; a search that did not get through is tried again.
func TestWunschStufen(t *testing.T) {
	ctx := context.Background()
	l, _, _ := list(t)
	l.Changed = nil // the searches tell from their own goroutine
	s := &fakeSuche{fund: musicbrainz.Fund{Gefunden: true, Album: "Amore", Datum: "2014-10-17", Label: "Problembär"}, calls: make(chan string, 4)}
	bib := fakeBib{}
	w := &Wuensche{Listen: l, MB: s, Bib: bib, Dir: t.TempDir()}
	if _, err := w.Wuenschen(ctx, merken.Lied{Kuenstler: "Wanda", Titel: "Bologna"}, "anna"); err != nil {
		t.Fatal(err)
	}
	if got := <-s.calls; got != "Wanda – Bologna" {
		t.Fatalf("searched %q", got)
	}
	items, _ := l.Andere(MusikListe).Lesen(ctx)
	uid := items[0].UID
	deadline := time.Now().Add(2 * time.Second)
	for w.Stand(uid).Fund == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if st := w.Stand(uid); st.Fund == nil || st.Fund.Album != "Amore" || st.Stufe != StufeWunsch {
		t.Fatalf("%+v", st)
	}

	said, err := w.Stufe(ctx, uid, "bestellt-lp")
	if err != nil || !strings.Contains(said, "als Platte mit Download") || w.Stand(uid).Stufe != StufeBestellt || !w.Stand(uid).LP {
		t.Fatalf("%q %v %+v", said, err, w.Stand(uid))
	}
	// the download is sorted in: the file is there, the record not yet
	bib["wanda|bologna"] = jellyfin.Treffer{Album: "Amore", Titel: "Bologna"}
	done, _ := w.Pruefen(ctx)
	if len(done) != 1 || !strings.Contains(done[0].Satz, "Die Platte ist noch unterwegs") || w.Stand(uid).Stufe != StufeDatei {
		t.Fatalf("%+v %+v", done, w.Stand(uid))
	}
	if again, _ := w.Pruefen(ctx); len(again) != 0 {
		t.Errorf("told twice: %+v", again)
	}
	if items, _ := l.Andere(MusikListe).Lesen(ctx); items[0].Done {
		t.Error("ticked before the record came")
	}
	said, err = w.Stufe(ctx, uid, "platte")
	if err != nil || !strings.Contains(said, "abgehakt") {
		t.Fatalf("%q %v", said, err)
	}
	if items, _ := l.Andere(MusikListe).Lesen(ctx); !items[0].Done {
		t.Error("not ticked when the record came")
	}
	// the file alone: "Datei ist da" ticks it off
	w.Wuenschen(ctx, merken.Lied{Kuenstler: "Wanda", Titel: "Salbei"}, "anna")
	<-s.calls
	items, _ = l.Andere(MusikListe).Lesen(ctx)
	uid2 := items[0].UID
	w.Stufe(ctx, uid2, "bestellt-datei")
	if said, _ := w.Stufe(ctx, uid2, "datei"); !strings.Contains(said, "ist da und abgehakt") {
		t.Errorf("%q", said)
	}
	if _, err := w.Stufe(ctx, "nix", "platte"); err != ErrNoLine {
		t.Errorf("unknown: %v", err)
	}

	// a search that did not get through is tried again after an hour
	s2 := &fakeSuche{err: errors.New("timeout"), calls: make(chan string, 4)}
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	w2 := &Wuensche{Listen: l, MB: s2, Dir: t.TempDir(), Now: func() time.Time { return now }}
	w2.Wuenschen(ctx, merken.Lied{Kuenstler: "Bilderbuch", Titel: "Bungalow"}, "")
	<-s2.calls
	for i := 0; i < 200 && w2.Stand(firstOpen(t, l)).Versucht.IsZero(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	w2.Nachsuchen(ctx)
	select {
	case got := <-s2.calls:
		t.Errorf("searched again at once: %q", got)
	default:
	}
	now = now.Add(61 * time.Minute)
	w2.Nachsuchen(ctx)
	if got := <-s2.calls; got != "Bilderbuch – Bungalow" {
		t.Errorf("again: %q", got)
	}
}

func firstOpen(t *testing.T, l *Liste) string {
	items, _ := l.Andere(MusikListe).Lesen(context.Background())
	for _, it := range items {
		if !it.Done {
			return it.UID
		}
	}
	t.Fatal("no open wish")
	return ""
}
