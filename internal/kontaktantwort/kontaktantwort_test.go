package kontaktantwort

import (
	"context"
	"encoding/base64"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func store(t *testing.T, now *time.Time) *Store {
	t.Helper()
	return &Store{Path: filepath.Join(t.TempDir(), Datei), Now: func() time.Time { return *now }}
}

var anna = Felder{Vorname: "Anna", Nachname: "Beispiel", Telefone: []Telefon{{"+49 151 23456789", "mobil"}}, Mails: []string{"anna@example.org"}}

func TestTheLinkIsLongRandomHashedAndWorksOnceForFourteenDays(t *testing.T) {
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	s := store(t, &now)
	token, f, err := s.Neu("mdopp", Haushalt, "h0", anna)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw)*8 < 128 || len(raw) != 32 {
		t.Fatalf("token %q: %d bits, want 256", token, len(raw)*8)
	}
	b, _ := os.ReadFile(s.Path)
	if strings.Contains(string(b), token) || !strings.Contains(string(b), f.Hash) {
		t.Fatalf("the store keeps the token, or not its hash:\n%s", b)
	}
	if fi, _ := os.Stat(s.Path); fi.Mode().Perm() != 0o600 {
		t.Errorf("store mode %v, want 0600", fi.Mode().Perm())
	}
	if got, err := s.Zum(token); err != nil || got.ID != f.ID || got.Bis.Sub(got.Am) != 14*24*time.Hour {
		t.Fatalf("Zum = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "x", token[:42], token + "A", strings.Repeat("A", 43)} {
		if _, err := s.Zum(bad); err != ErrWeg {
			t.Errorf("Zum(%q) = %v, want ErrWeg", bad, err)
		}
	}
	if _, err := s.Beantworten(token, Antwort{Bestaetigt: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Zum(token); err != ErrWeg {
		t.Errorf("an answered link still opens: %v", err)
	}
	if _, err := s.Beantworten(token, Antwort{Bestaetigt: true}); err != ErrWeg {
		t.Errorf("a second answer was taken: %v", err)
	}

	token2, _, _ := s.Neu("mdopp", Privat, "k1", anna)
	now = now.Add(14*24*time.Hour - time.Second)
	if _, err := s.Zum(token2); err != nil {
		t.Errorf("a link a second before its end: %v", err)
	}
	now = now.Add(time.Second)
	if _, err := s.Zum(token2); err != ErrWeg {
		t.Errorf("a link after 14 days still opens: %v", err)
	}
}

func TestANewLinkForTheSameContactReplacesTheOpenOne(t *testing.T) {
	now := time.Now()
	s := store(t, &now)
	old, _, _ := s.Neu("mdopp", Haushalt, "h0", anna)
	neu, _, _ := s.Neu("mdopp", Haushalt, "h0", anna)
	if _, err := s.Zum(old); err != ErrWeg {
		t.Errorf("the old link still opens: %v", err)
	}
	if _, err := s.Zum(neu); err != nil {
		t.Errorf("the new one does not: %v", err)
	}
}

func TestTheReviewIsTheStartersAndAHandyKontaktWaitsForThePhone(t *testing.T) {
	now := time.Now()
	s := store(t, &now)
	token, f, _ := s.Neu("mdopp", Privat, "lookup.1", anna)
	nachher := anna
	nachher.Telefone = []Telefon{{"+49 151 99999999", "mobil"}}
	nachher.Mails = []string{"anna@example.org", "anna@beispiel.example"}
	if _, err := s.Beantworten(token, Antwort{Aenderungen: Vergleich(anna, nachher)}); err != nil {
		t.Fatal(err)
	}
	if n := len(s.ZuPruefen("mdopp")); n != 1 {
		t.Fatalf("mdopp has %d to review", n)
	}
	if n := len(s.ZuPruefen("ben")); n != 0 {
		t.Errorf("ben sees mdopp's answers: %d", n)
	}
	if _, err := s.Eine(f.ID, "ben"); err != ErrWeg {
		t.Errorf("ben opens mdopp's review: %v", err)
	}
	got, _ := s.Eine(f.ID, "mdopp")
	if len(got.Aenderungen) != 2 {
		t.Fatalf("changes %+v", got.Aenderungen)
	}
	if len(s.FuersHandy("mdopp")) != 0 {
		t.Error("an undecided answer goes to the phone")
	}
	if _, err := s.Entscheiden(f.ID, "mdopp", Stand(got, false, []int{1})); err != nil {
		t.Fatal(err)
	}
	if len(s.ZuPruefen("mdopp")) != 0 {
		t.Error("a decided answer is still a task")
	}
	fh := s.FuersHandy("mdopp")
	if len(fh) != 1 || fh[0].Aenderungen[0].Stand != Abgelehnt || fh[0].Aenderungen[1].Stand != Angenommen {
		t.Fatalf("for the phone: %+v", fh)
	}
	if _, err := s.VomHandy(f.ID, "ben", []int{1}); err != ErrWeg {
		t.Errorf("ben's phone marks mdopp's: %v", err)
	}
	done, err := s.VomHandy(f.ID, "mdopp", []int{1})
	if err != nil || done.Aenderungen[1].Stand != Uebernommen || done.Aenderungen[0].Stand != Abgelehnt {
		t.Fatalf("VomHandy = %+v, %v", done, err)
	}
	if len(s.FuersHandy("mdopp")) != 0 {
		t.Error("a written correction is sent again")
	}
}

func TestADiscardedAnswerNeverReachesThePhone(t *testing.T) {
	now := time.Now()
	s := store(t, &now)
	token, f, _ := s.Neu("mdopp", Privat, "k", anna)
	nachher := anna
	nachher.Nachname = "Muster"
	s.Beantworten(token, Antwort{Aenderungen: Vergleich(anna, nachher)})
	got, _ := s.Eine(f.ID, "mdopp")
	s.Entscheiden(f.ID, "mdopp", Stand(got, false, nil))
	if len(s.FuersHandy("mdopp")) != 0 {
		t.Error("a discarded answer goes to the phone")
	}
}

func TestOldEntriesGo(t *testing.T) {
	now := time.Now()
	s := store(t, &now)
	token, f, _ := s.Neu("mdopp", Haushalt, "h", anna)
	s.Beantworten(token, Antwort{Bestaetigt: true})
	got, _ := s.Eine(f.ID, "mdopp")
	s.Entscheiden(f.ID, "mdopp", Stand(got, true, nil))
	now = now.Add(Aufbewahren + time.Hour)
	if _, err := s.Eine(f.ID, "mdopp"); err != ErrWeg {
		t.Errorf("a review decided 30 days ago is still kept: %v", err)
	}
}

func TestVergleichPlaceByPlace(t *testing.T) {
	vorher := Felder{Vorname: "Anna", Telefone: []Telefon{{"0171 1234567", "mobil"}, {"0711 123", "privat"}, {"0711 9", "privat"}}, Mails: []string{"a@x.de"}, Firma: "A",
		Adressen: []Adresse{{Strasse: "Weg 1", PLZ: "12345", Ort: "Stadt"}}, Geburtstag: "1980-01-02"}
	nachher := Felder{Vorname: "Anna", Telefone: []Telefon{{"0171-1234567", "mobil"}, {"", ""}, {"0711 9", "geschaeftlich"}, {"0151 7654321", "mobil"}}, Mails: []string{"A@X.de"}, Firma: "A", Position: "Chefin",
		Adressen: []Adresse{{Strasse: "Weg 2", PLZ: "12345", Ort: "Stadt"}, {}}, Geburtstag: "1980-01-03"}
	got := Vergleich(vorher, nachher)
	var arts []string
	for _, a := range got {
		arts = append(arts, a.Art+":"+a.Vorher+">"+a.Nachher)
	}
	want := "telefon:0711 123> telefon:0711 9>0711 9 telefon:>0151 7654321 adresse:Weg 1, 12345 Stadt>Weg 2, 12345 Stadt geburtstag:1980-01-02>1980-01-03 position:>Chefin"
	if strings.Join(arts, " ") != want {
		t.Errorf("Vergleich\n got %s\nwant %s", strings.Join(arts, " "), want)
	}
	if len(Vergleich(vorher, vorher)) != 0 {
		t.Error("the same fields are a change")
	}
}

func TestNummer(t *testing.T) {
	for in, ok := range map[string]bool{
		"+49 151 23456789": true, "0171 1234567": true, "0711 123450": true, "+49 (0)711 123450": true, "0049 30 1234567": true,
		"+43 660 1234567": true, "+1 415 555 0100": true, "0800 1234567": true,
		"0171 12345": false, "+49 171 1234567 00": false, "1234567": false, "+28 123 456789": false, "0171 abc": false,
		"01 71+": false, "+49 30 12": false, "+44 12": false,
	} {
		if h := Nummer(in, "49"); (h == "") != ok {
			t.Errorf("Nummer(%q) = %q, want ok=%v", in, h, ok)
		}
	}
}

type dns struct {
	mx    map[string][]*net.MX
	hosts map[string][]string
	slow  bool
}

func (d dns) LookupMX(ctx context.Context, n string) ([]*net.MX, error) {
	if d.slow {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if mx, ok := d.mx[n]; ok {
		return mx, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: n, IsNotFound: true}
}

func (d dns) LookupHost(ctx context.Context, n string) ([]string, error) {
	if h, ok := d.hosts[n]; ok {
		return h, nil
	}
	return nil, &net.DNSError{Err: "no such host", Name: n, IsNotFound: true}
}

func TestMail(t *testing.T) {
	p := Pruefung{DNS: dns{mx: map[string][]*net.MX{"example.org": {{Host: "mx.example.org."}}, "null.example": {{Host: "."}}},
		hosts: map[string][]string{"a-only.example": {"192.0.2.1"}}}}
	for in, want := range map[string]string{
		"anna@example.org": "", "Anna@Example.org": "", "x@a-only.example": "",
		"anna@": "nicht gültig", "anna example.org": "nicht gültig", "Anna <a@example.org>": "nicht gültig", "a@b": "nicht gültig", "a@b.c1": "nicht gültig",
		"a@nowhere.example": "gibt es nicht", "a@null.example": "ausdrücklich keine",
	} {
		h := p.Mail(context.Background(), in)
		if (want == "") != (h == "") || !strings.Contains(h, want) {
			t.Errorf("Mail(%q) = %q, want %q", in, h, want)
		}
	}
	DNSWarten = 50 * time.Millisecond
	defer func() { DNSWarten = 3 * time.Second }()
	began := time.Now()
	h := Pruefung{DNS: dns{slow: true}}.Mail(context.Background(), "a@slow.example")
	if !strings.Contains(h, "nicht prüfen") || time.Since(began) > time.Second {
		t.Errorf("a slow DNS: %q after %v", h, time.Since(began))
	}
}

func TestGeburtstagNameUndViele(t *testing.T) {
	now := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	for in, ok := range map[string]bool{"1980-02-29": true, "--12-24": true, "2027-01-01": false, "1890-01-01": false, "1981-02-29": false, "31.12.1980": false} {
		if h := Geburtstag(in, now); (h == "") != ok {
			t.Errorf("Geburtstag(%q) = %q", in, h)
		}
	}
	for _, c := range [][3]string{{"Anna Beispiel", "Anna Muster", ""}, {"Anna Beispiel", "Anne Beispiel", ""}, {"Anna Beispiel", "Karl-Heinz Otto", "ganz anders"}, {"", "Neu", ""}} {
		if h := Name(c[0], c[1]); !strings.Contains(h, c[2]) || (c[2] == "") != (h == "") {
			t.Errorf("Name(%q, %q) = %q", c[0], c[1], h)
		}
	}
	if h := (Pruefung{Now: now}).Pruefen(context.Background(), Felder{Vorname: "Anna", Nachname: "Beispiel"}, Felder{Vorname: "Karl-Heinz", Nachname: "Otto"}, nil); len(h) != 1 || !strings.Contains(h[0], "ganz anders") {
		t.Errorf("a name changed completely: %q", h)
	}
	as := make([]Aenderung, VieleAb)
	for i := range as {
		as[i] = Aenderung{Art: "telefon", Nachher: "0171 1234567"}
	}
	if h := (Pruefung{Now: now}).Pruefen(context.Background(), Felder{}, Felder{}, as); len(h) != 1 || !strings.Contains(h[0], "Viele Änderungen") {
		t.Errorf("five changes: %q", h)
	}
	if h := (Pruefung{Now: now}).Pruefen(context.Background(), Felder{}, Felder{}, as[:2]); h != nil {
		t.Errorf("two changes flagged: %q", h)
	}
}

func TestSauberCutsAndFlattens(t *testing.T) {
	long := strings.Repeat("ä", GrenzeWert+5)
	var many []Telefon
	for range GrenzeJeArt + 3 {
		many = append(many, Telefon{"0171 1234567", "fax<script>"})
	}
	got := Sauber(Felder{Vorname: "Anna\r\nBCC: x", Telefone: many, Mails: []string{long}})
	if got.Vorname != "Anna BCC: x" || len(got.Telefone) != GrenzeJeArt || got.Telefone[0].Art != "" || len([]rune(got.Mails[0])) != GrenzeWert {
		t.Errorf("Sauber = %q, %d, %+v, %d", got.Vorname, len(got.Telefone), got.Telefone[0], len([]rune(got.Mails[0])))
	}
}
