package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/homes/internal/devices"
	"github.com/mdopp/homes/internal/laib"
	"github.com/mdopp/homes/internal/modul"
	"github.com/mdopp/homes/internal/usage"
)

// a Laib answering person_pending and zuhause as given; "" is a tool
// error, "nein" a hand that may not
func haushaltLaib(t *testing.T, pending, zuhause string) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := map[string]string{"laib_person_pending": pending, "laib_zuhause": zuhause}[req.Params.Name]
		isErr := text == "" || text == "nein"
		if text == "nein" {
			text = "homes may not " + req.Params.Name
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

// owner 2026-11-06: Haushalt is five rows with their state, each leading
// to its page; Haushalt is marked in the four tabs.
func TestBoxHaushaltSaysEachRowsState(t *testing.T) {
	store, err := devices.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"Pixel", "Fairphone", "iPhone"} {
		store.Mint(n, "anna")
	}
	l, err := modul.Bauen([]modul.Modul{{Name: "musik", Titel: "Musik"}, {Name: "fotos", Titel: "Fotos"}, {Name: "standort", Titel: "Standort", Warum: modul.Fehlt, Grund: "kein Reitti"}})
	if err != nil {
		t.Fatal(err)
	}
	counter := &usage.Counter{URL: "http://127.0.0.1:1", Started: time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local)}
	cfg := BoxConfig{Laib: haushaltLaib(t, `[{"user":"oma","name":"Oma Hilde"}]`, `{"street":"Musterweg 1","city":"Musterstadt"}`),
		Devices: store, Family: StartConfig{Module: l, Usage: counter}}
	page := getAt(BoxPage(cfg), "/box/haushalt?ganz=1", "admins")
	mustContain(t, page, "<h1>Haushalt</h1>", `href=/box/haushalt aria-current=page>Haushalt</a>`,
		`href="/box/personen"`, "1 Einladung offen", `href="/box/geraete"`, "3 Telefone gekoppelt", `href="/adresse"`, "Adresse gesetzt",
		`href="/box/module"`, "2 von 3 Modulen an", `href="/box/nutzung"`, "Homes zählt seit 02.11.")
	before(t, page, "Personen", "Geräte")
	before(t, page, "Geräte", "Adresse gesetzt")
	before(t, page, "Adresse gesetzt", "Modulen an")
	before(t, page, "Modulen an", "Homes zählt")

	// nobody pending, no address yet; without counting and the module list those rows are not there
	cfg = BoxConfig{Laib: haushaltLaib(t, `[]`, `{}`)}
	page = getAt(BoxPage(cfg), "/box/haushalt?ganz=1", "admins")
	mustContain(t, page, "Jemanden einladen", "Noch keine Adresse")
	mustNotContain(t, page, "Telefone", "Modulen an", "Homes zählt", `href="/box/nutzung"`)

	// a hand without the verbs, and a Laib that fails: said in the row
	page = getAt(BoxPage(BoxConfig{Laib: haushaltLaib(t, "nein", "")}), "/box/haushalt?ganz=1", "admins")
	mustContain(t, page, "Jemanden einladen", "lässt sich gerade nicht lesen")
	page = getAt(BoxPage(BoxConfig{Laib: haushaltLaib(t, "", "")}), "/box/haushalt?ganz=1", "admins")
	if n := strings.Count(page, "lässt sich gerade nicht lesen"); n != 2 {
		t.Errorf("a failing Laib in %d rows:\n%s", n, page)
	}
	if rec := boxAt(BoxPage(cfg), "/box/haushalt", "family"); rec.Code != http.StatusSeeOther {
		t.Errorf("the family sees Haushalt: %d", rec.Code)
	}
}
