package pages

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/persona"
)

// haus#58 (#35's rule on the admin pages too): the persona speaks where you
// talk to it, nowhere else. Box, Geräte and the app's pairing page wear the
// family pages' frame — the house palette, Haus's Geist, the tab bar with
// Box — and no persona name, greeting, icon or colours.
func TestTheAdminPagesWearTheFamilyFrameNotThePersona(t *testing.T) {
	House = persona.Persona{Display: "Joshua", Greeting: map[string]string{"de": "Hallo, hier ist Joshua. Wollen wir ein Spiel spielen?"},
		Colors: map[string]string{"accent": "#3ddc84", "ground": "#0c120e"}, Icon: []byte("<svg/>")}
	defer func() { House = persona.Default() }()
	store, _ := devices.Open(t.TempDir())
	_, hc := house(t)
	family := StartConfig{HA: hc}
	for _, p := range []struct {
		h     http.HandlerFunc
		path  string
		title string
	}{
		{BoxPage(BoxConfig{Laib: fakeLaib(t, map[string]any{"state": "bereit"}, nil), LaibURL: "https://laib.box.test", Family: family}), "/box", "Box · Zuhause"},
		{Geraete(store, "https://haus.example", family), "/box/geraete", "Geräte · Zuhause"},
		{PairDevice(store, family), "/pair-device", "Telefon koppeln · Zuhause"},
	} {
		req := httptest.NewRequest("GET", p.path, nil)
		req.Header.Set("Remote-User", "anna")
		req.Header.Set("Remote-Groups", "admins")
		rec := httptest.NewRecorder()
		p.h.ServeHTTP(rec, req)
		body := rec.Body.String()
		mustContain(t, body, "<title>"+p.title+"</title>", "#070709", "#14141a", "#f97316", "prefers-color-scheme:light", "@font-face", FontPath,
			"<nav class=tabs", "href=/box aria-current=page", "href=/raeume")
		mustNotContain(t, body, "Joshua", "Wollen wir", "Hallo, hier ist", "#3ddc84", "#0c120e", "/persona/icon.svg", "<header>", "#faf9f6", "class=gruss")
	}
	rec := httptest.NewRecorder()
	Icon().ServeHTTP(rec, httptest.NewRequest("GET", "/persona/icon.svg", nil))
	if rec.Body.String() != "<svg/>" || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("icon: %q %q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
}
