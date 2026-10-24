package pages

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Every family page carries the one Haus script in its head, before the
// selection, so a page swapped in keeps it; the pages that change by
// themselves carry their interval, the others none. The script holds
// nothing a leak probe looks for.
func TestFamilyPagesCarryTheHausScript(t *testing.T) {
	_, hc := house(t)
	_, cal := calendar(t)
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	all := StartConfig{HA: hc, Calendar: cal, Notes: nd}
	for _, p := range []struct {
		page  func(StartConfig) http.HandlerFunc
		path  string
		every string // "": none
	}{{Start, "/start", "15"}, {Raeume, "/raeume", "15"}, {Medien, "/medien", "10"}, {Energie, "/energie", "60"},
		{Kalender, "/kalender", ""}, {Notizen, "/notizen", ""}, {Mehr, "/mehr", ""}} {
		_, body, _ := familyPage(t, p.page, all, "GET", p.path, "family")
		mustContain(t, body, hausScript, hausStyle)
		before(t, body, hausScript, "<nav")
		if p.every != "" {
			mustContain(t, body, `<div hidden data-auffrischen=`+p.every+`></div>`)
		} else {
			mustNotContain(t, body, `<div hidden data-auffrischen`)
		}
	}
	mustNotLeak(t, hausScript+hausStyle)
	if strings.Contains(hausScript, "|") {
		t.Error("the script holds a |, which leak probes of note tables look for")
	}
}

// A switch the script sent (X-Haus-Skript) that fails is answered in a
// sentence, text/plain with the refusal's code or 502, so the page stays
// and shows it; the same switch sent by the browser itself still gets
// the page with the sentence.
func TestSchaltenAnswersTheScriptInASentence(t *testing.T) {
	fake, cfg := switchHouse(t)
	h := Schalten(cfg)
	script := map[string]string{"X-Haus-Skript": "1"}
	fake.FailCalls(503)
	rec := post(t, h, "/schalten", url.Values{"geraet": {"light.flur"}, "tun": {"light.turn_on"}, "zurueck": {"/start"}}, "anna", script)
	if rec.Code != http.StatusBadGateway || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") || strings.TrimSpace(rec.Body.String()) != saysSwitchFailed {
		t.Fatalf("%d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	rec = post(t, h, "/schalten", url.Values{"geraet": {"lock.haustuer"}, "tun": {"lock.unlock"}, "zurueck": {"/start"}}, "anna", script)
	if rec.Code != http.StatusBadRequest || strings.TrimSpace(rec.Body.String()) != saysNotSwitched {
		t.Fatalf("refusal: %d %q", rec.Code, rec.Body)
	}
	rec = post(t, h, "/schalten", url.Values{"geraet": {"light.flur"}, "tun": {"light.turn_on"}, "zurueck": {"/start"}}, "anna", nil)
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || !strings.Contains(rec.Body.String(), saysSwitchFailed) {
		t.Fatalf("browser's own: %q", rec.Header().Get("Content-Type"))
	}
	fake.FailCalls(0)
	rec = post(t, h, "/schalten", url.Values{"geraet": {"light.flur"}, "tun": {"light.turn_on"}, "zurueck": {"/raeume#karte-flur"}}, "anna", script)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/raeume#karte-flur" {
		t.Fatalf("done: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

// What waits says so on its button, what removes asks first, an address
// to copy has its button.
func TestButtonsSayWhatRuns(t *testing.T) {
	nd := notesDir(t, map[string]time.Duration{"Einkaufsliste": time.Hour})
	_, body, _ := familyPage(t, Notizen, StartConfig{Notes: nd}, "GET", "/notizen", "family")
	mustContain(t, body, `<button data-laeuft="Sucht …">Suchen</button>`, `<button data-laeuft="Legt an …">Anlegen</button>`)
	mustContain(t, copyField("https://caldav.box.test/anna/privat/"), `<button type=button data-kopieren>Kopieren</button>`)
}
