package einrichten

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
)

func box(t *testing.T, ms ...modul.Modul) *Quellen {
	t.Helper()
	l, err := modul.Bauen(ms)
	if err != nil {
		t.Fatal(err)
	}
	return &Quellen{Module: l, Haken: Open(t.TempDir()), Adresse: "https://homes.box.test"}
}

var (
	fotos = modul.Modul{Name: "fotos", Titel: "Fotos", Einrichtung: App("Immich", "app.alextran.immich", "https://photos.box.test", "https://immich.app/docs")}
	filme = modul.Modul{Name: "filme", Titel: "Filme und Musik", Einrichtung: App("Jellyfin", "org.jellyfin.mobile", "https://media.box.test", "https://jellyfin.org/docs")}
	notiz = modul.Modul{Name: "notizen", Titel: "Notizen", Nav: []modul.Eintrag{{Titel: "Notizen", Pfad: "/notizen"}}}
	ort   = modul.Modul{Name: "standort", Titel: "Standort", Einrichtung: &modul.Einrichtung{Web: "https://reitti.box.test"}}
	aus   = modul.Modul{Name: "passwoerter", Titel: "Passwörter", Warum: modul.Fehlt, Grund: "nicht da", Einrichtung: App("Bitwarden", "com.x8bit.bitwarden", "https://vault.box.test", "x")}
)

func offen(q *Quellen, person string) []string {
	var out []string
	for _, a := range q.Fuer(person) {
		if a.Erledigt == nil {
			out = append(out, a.Modul)
		}
	}
	return out
}

// A module that is on and has its own app is a task, open while nothing
// says otherwise; one without an app, or off, is none.
func TestATaskForEachModuleWithAnApp(t *testing.T) {
	q := box(t, fotos, notiz, ort, aus)
	as := q.Fuer("anna")
	if len(as) != 1 || as[0].Modul != "fotos" || as[0].Titel != "Fotos auf dem Handy einrichten" || as[0].Warum != "Mit der App „Immich“." || as[0].Erledigt != nil {
		t.Fatalf("tasks: %+v", as)
	}
	if len(offen(q, "anna")) != 1 || q.Fuer("") != nil || (*Quellen)(nil).Fuer("anna") != nil {
		t.Error("count, nobody, no sources")
	}
}

// The phone's installed apps tick the open tasks whose app it has, once;
// the others stay open, and what is kept survives a new Quellen.
func TestInstalledAppsTickTheirTasks(t *testing.T) {
	q := box(t, fotos, filme)
	n, err := q.Installiert("anna", []string{"org.jellyfin.mobile", "com.whatsapp"})
	if err != nil || n != 1 {
		t.Fatalf("ticked %d: %v", n, err)
	}
	if got := offen(q, "anna"); len(got) != 1 || got[0] != "fotos" {
		t.Errorf("open: %v", got)
	}
	if got := offen(q, "ben"); len(got) != 2 {
		t.Errorf("ben's are his own: %v", got)
	}
	if n, _ := q.Installiert("anna", []string{"org.jellyfin.mobile"}); n != 0 {
		t.Errorf("ticked again: %d", n)
	}
	b, _ := os.ReadFile(q.Haken.path)
	var kept map[string]map[string]Stand
	if json.Unmarshal(b, &kept) != nil || kept["anna"]["filme"].Wie != Handy || kept["anna"]["filme"].Am == "" {
		t.Errorf("kept: %s", b)
	}
}

// A tick by hand holds; a module that is gone has no task, and comes back
// done (its tick is kept).
func TestATickByHandAndAModuleGone(t *testing.T) {
	dir := t.TempDir()
	q := box(t, fotos, filme)
	q.Haken = Open(dir)
	if err := q.Haken.Setzen("anna", "fotos", Hand, time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	q.Haken.Setzen("anna", "fotos", Handy, time.Now()) // the first Stand stays
	if a, _ := q.Eine("anna", "fotos"); a.Erledigt == nil || a.Erledigt.Wie != Hand || a.Erledigt.Am != "2026-11-04" {
		t.Errorf("fotos: %+v", a.Erledigt)
	}
	without := box(t, filme)
	without.Haken = Open(dir)
	if _, ok := without.Eine("anna", "fotos"); ok || len(without.Fuer("anna")) != 1 {
		t.Errorf("a module gone keeps its task: %+v", without.Fuer("anna"))
	}
	again := box(t, fotos, filme)
	again.Haken = Open(dir)
	if a, _ := again.Eine("anna", "fotos"); a.Erledigt == nil {
		t.Error("back again, open again")
	}
	if err := (*Haken)(nil).Setzen("anna", "fotos", Hand, time.Now()); err == nil {
		t.Error("no ticks kept, and it said done")
	}
	os.WriteFile(filepath.Join(dir, File), []byte("{kaputt"), 0o600)
	if err := q.Haken.Setzen("anna", "filme", Hand, time.Now()); err == nil {
		t.Error("a broken file was written over")
	}
}

// The Homes app's task is done once the person paired a phone; a
// module's own check ticks it; one that fails leaves it open.
func TestPairedAndDetected(t *testing.T) {
	q := box(t, fotos, filme)
	q.HomesApp = App("Homes", "cloud.dopp.homes", "https://homes.box.test", "x", "schritt.homes")
	q.Gekoppelt = func(p string) bool { return p == "anna" }
	q.Erkennen = map[string]func(context.Context, string) (bool, error){
		"filme": func(_ context.Context, p string) (bool, error) { return p == "anna", nil },
		"fotos": func(context.Context, string) (bool, error) { return true, errors.New("immich antwortet nicht") },
	}
	if a, _ := q.Eine("anna", Homes); a.Erledigt == nil || a.Erledigt.Wie != Gekoppelt || a.Titel != "Homes-App auf dem Handy einrichten" {
		t.Errorf("homes: %+v", a)
	}
	if a, _ := q.Eine("ben", Homes); a.Erledigt != nil {
		t.Error("ben paired nothing")
	}
	q.Pruefen(context.Background(), "anna")
	q.Pruefen(context.Background(), "ben")
	if got := offen(q, "anna"); len(got) != 1 || got[0] != "fotos" {
		t.Errorf("anna open: %v", got)
	}
	if got := offen(q, "ben"); len(got) != 3 {
		t.Errorf("ben open: %v", got)
	}
}

// GET /napi/einrichtung's answer: the modules on, for everyone, with an
// address or an app; each app's task; the open count is the open tasks;
// „Öffnen“ and the icon as each module says (owner 2026-11-07).
func TestNapiAnswer(t *testing.T) {
	zuOeffnen := notiz
	zuOeffnen.Oeffnen, zuOeffnen.Icon = true, "notiz"
	q := box(t, fotos, filme, zuOeffnen, ort, aus, modul.Modul{Name: "werkstatt", Admins: true, Nav: []modul.Eintrag{{Pfad: "/box"}}}, modul.Modul{Name: "konto"})
	q.Ziele = map[string]string{"einkauf": "https://homes.box.test/einkauf"}
	q.Installiert("anna", []string{"org.jellyfin.mobile"})
	got := q.Napi("anna")
	b, _ := json.Marshal(got)
	want := `{"version":1,"offen":1,"module":[` +
		`{"name":"fotos","titel":"Fotos","icon":"fotos","oeffnen":false,"web":"https://photos.box.test","app":{"name":"Immich","paket":"app.alextran.immich","store":"https://play.google.com/store/apps/details?id=app.alextran.immich"},"nur_web":false,` +
		`"aufgabe":{"titel":"Fotos auf dem Handy einrichten","erledigt":false,"einrichten":"https://homes.box.test/einrichten/fotos"}},` +
		`{"name":"filme","titel":"Filme und Musik","icon":"filme","oeffnen":false,"web":"https://media.box.test","app":{"name":"Jellyfin","paket":"org.jellyfin.mobile","store":"https://play.google.com/store/apps/details?id=org.jellyfin.mobile"},"nur_web":false,` +
		`"aufgabe":{"titel":"Filme und Musik auf dem Handy einrichten","erledigt":true,"am":"` + time.Now().Format("2006-01-02") + `","wie":"app","einrichten":"https://homes.box.test/einrichten/filme"}},` +
		`{"name":"notizen","titel":"Notizen","icon":"notiz","oeffnen":true,"web":"https://homes.box.test/notizen","nur_web":true},` +
		`{"name":"standort","titel":"Standort","icon":"standort","oeffnen":false,"web":"https://reitti.box.test","nur_web":true}],` +
		`"ziele":{"einkauf":"https://homes.box.test/einkauf"}}`
	if string(b) != want {
		t.Errorf("got\n%s\nwant\n%s", b, want)
	}
}

func TestDomainAndAddresses(t *testing.T) {
	if Domain("https://homes.dopp.cloud") != "dopp.cloud" || Domain("https://www.dopp.cloud") != "dopp.cloud" || Domain("") != "" || Domain("https://localhost:8650") != "" {
		t.Errorf("Domain: %q %q %q", Domain("https://homes.dopp.cloud"), Domain(""), Domain("https://localhost:8650"))
	}
	if Web("photos", "dopp.cloud") != "https://photos.dopp.cloud" || Web("photos", "") != "" || App("x", "y", "", "z") != nil {
		t.Error("Web, App without an address")
	}
	if Signal("+49 170 1") != "https://signal.me/#p/+49%20170%201" || Signal("") != "" {
		t.Errorf("Signal: %q", Signal("+49 170 1"))
	}
}

// Dienste: the services Laib runs, asked once; Laib down, nil (cmd/homes'
// dienstePflegen asks again; until then the link modules are not listed).
func TestDienste(t *testing.T) {
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		text := `{"ready":{"state":"bereit"},"services":[{"name":"immich","state":"grün"},{"name":"media","state":"rot"}]}`
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	defer srv.Close()
	got := Dienste(context.Background(), &laib.Client{URL: srv.URL, Token: "t"}, time.Second)
	if !got["immich"] || !got["media"] || got["vaultwarden"] || asked != 1 {
		t.Errorf("dienste %v after %d asks", got, asked)
	}
	srv.Close()
	if got := Dienste(context.Background(), &laib.Client{URL: srv.URL, Token: "t"}, 100*time.Millisecond); got != nil {
		t.Errorf("Laib down: %v", got)
	}
}

// The key owner's open questions in Immich are one task while any is open
// (owner 2026-11-07): with the count as it is now, for them only, in
// /napi/einrichtung as an entry of its own leading to the questions; none
// once nothing is open, nor with the fotos module off.
func TestImmichFragenAlsAufgabe(t *testing.T) {
	q := box(t, fotos)
	n := 3
	q.Immich = func(p string) int {
		if p != "anna" {
			return 0
		}
		return n
	}
	fr := q.Fragen("anna")
	if len(fr) != 1 || fr[0].Modul != ImmichPruefen || fr[0].Titel != "3 Personen in Immich prüfen" || q.Fragen("ben") != nil {
		t.Fatalf("%+v", fr)
	}
	a := q.Napi("anna")
	last := a.Module[len(a.Module)-1]
	if a.Offen != 2 || last.Name != ImmichPruefen || last.Icon != "fotos" || !last.NurWeb || last.App != nil || last.Web != "https://homes.box.test/kontakte/dieselbe" ||
		last.Aufgabe == nil || last.Aufgabe.Erledigt || last.Aufgabe.Einrichten != last.Web || last.Aufgabe.Titel != "3 Personen in Immich prüfen" {
		t.Fatalf("%d %+v %+v", a.Offen, last, last.Aufgabe)
	}
	if b := q.Napi("ben"); b.Offen != 1 || b.Module[len(b.Module)-1].Name == ImmichPruefen {
		t.Fatalf("ben: %+v", b)
	}
	n = 1
	if fr := q.Fragen("anna"); fr[0].Titel != "1 Person in Immich prüfen" {
		t.Fatal(fr[0].Titel)
	}
	n = 0
	if q.Fragen("anna") != nil || q.Napi("anna").Offen != 1 {
		t.Fatal("a task with nothing open")
	}
	n = 2
	off := box(t, modul.Modul{Name: "fotos", Titel: "Fotos", Warum: modul.Fehlt, Grund: "kein Immich"})
	if off.Immich = q.Immich; off.Fragen("anna") != nil {
		t.Fatal("a task with fotos off")
	}
	if _, ok := q.Eine("anna", ImmichPruefen); ok {
		t.Fatal("the questions are no module's setup")
	}
}
