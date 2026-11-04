package napi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mdopp/homes/internal/einrichten"
	"github.com/mdopp/homes/internal/modul"
)

// The app's /napi/einrichtung (owner 2026-11-04): the device's person's
// modules with their tasks and the open count; the phone's installed apps
// tick theirs; a paired device only; without the tasks, not served.
func TestEinrichtungForTheApp(t *testing.T) {
	l, err := modul.Bauen([]modul.Modul{
		{Name: "fotos", Titel: "Fotos", Einrichtung: einrichten.App("Immich", "app.alextran.immich", "https://photos.box.test", "x")},
		{Name: "filme", Titel: "Filme und Musik", Einrichtung: einrichten.App("Jellyfin", "org.jellyfin.mobile", "https://media.box.test", "x")},
	})
	if err != nil {
		t.Fatal(err)
	}
	q := &einrichten.Quellen{Module: l, Haken: einrichten.Open(t.TempDir()), Adresse: "https://homes.box.test"}
	h := Handler(Config{Einrichtung: q, Validate: StaticValidator([]string{"dev1"}), Whoami: func(b string) (string, bool) { return "anna", b == "dev1" }})
	read := func(code int, body string) einrichten.Antwort {
		t.Helper()
		var a einrichten.Antwort
		if code != http.StatusOK || json.Unmarshal([]byte(body), &a) != nil {
			t.Fatalf("→ %d %s", code, body)
		}
		return a
	}
	a := read(raw(t, h, "GET", "/napi/einrichtung", "dev1", ""))
	if a.Offen != 2 || len(a.Module) != 2 || a.Module[0].App.Paket != "app.alextran.immich" || a.Module[0].Aufgabe.Erledigt {
		t.Fatalf("before: %+v", a)
	}
	a = read(raw(t, h, "POST", "/napi/einrichtung/apps", "dev1", `{"pakete":["org.jellyfin.mobile","com.example.other"]}`))
	if a.Offen != 1 || !a.Module[1].Aufgabe.Erledigt || a.Module[1].Aufgabe.Wie != einrichten.Handy {
		t.Fatalf("after the apps: %+v", a.Module[1].Aufgabe)
	}
	if a = read(raw(t, h, "GET", "/napi/einrichtung", "dev1", "")); a.Offen != 1 {
		t.Errorf("kept: %d open", a.Offen)
	}
	if code, body := raw(t, h, "POST", "/napi/einrichtung/apps", "dev1", `kaputt`); code != http.StatusBadRequest {
		t.Errorf("a broken list: %d %s", code, body)
	}
	for _, p := range []struct{ method, path string }{{"GET", "/napi/einrichtung"}, {"POST", "/napi/einrichtung/apps"}} {
		if code, _ := raw(t, h, p.method, p.path, "", ""); code != http.StatusUnauthorized {
			t.Errorf("%s %s without a device: %d", p.method, p.path, code)
		}
		if code, _ := raw(t, Handler(Config{Validate: StaticValidator([]string{"dev1"})}), p.method, p.path, "dev1", "{}"); code != http.StatusNotFound {
			t.Errorf("%s %s without tasks: %d", p.method, p.path, code)
		}
	}
}
