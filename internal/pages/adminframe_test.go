package pages

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mdopp/homes/internal/devices"
)

// haus#58 (#35's rule on the admin pages too): the assistant's name
// appears where you talk to it, nowhere else. Box, Geräte and the app's pairing page wear the
// family pages' frame — the house palette, Homes' Geist, the tab bar with
// Box — and no assistant's name.
func TestTheAdminPagesWearTheFamilyFrame(t *testing.T) {
	named(t, "Ada")
	store, _ := devices.Open(t.TempDir())
	_, hc := house(t)
	family := StartConfig{HA: hc}
	for _, p := range []struct {
		h     http.HandlerFunc
		path  string
		title string
	}{
		{BoxPage(BoxConfig{Laib: fakeLaib(t, map[string]any{"state": "bereit"}, nil), LaibURL: "https://laib.box.test", Family: family}), "/box", "Box · Homes"},
		{Geraete(store, "https://homes.example", family), "/box/geraete", "Geräte · Homes"},
		{PairDevice(store, family), "/pair-device", "Telefon koppeln · Homes"},
	} {
		req := httptest.NewRequest("GET", p.path, nil)
		req.Header.Set("Remote-User", "anna")
		req.Header.Set("Remote-Groups", "admins")
		rec := httptest.NewRecorder()
		p.h.ServeHTTP(rec, req)
		body := rec.Body.String()
		mustContain(t, body, "<title>"+p.title+"</title>", "#070709", "#14141a", "#f97316", "prefers-color-scheme:light", "@font-face", FontPath,
			"<nav class=tabs", "href=/box aria-current=page", "href=/raeume")
		mustNotContain(t, body, "Ada", "<header>")
	}
}
