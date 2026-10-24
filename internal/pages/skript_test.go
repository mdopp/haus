package pages

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
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
		every string // the marker after data-auffrischen=; "": none
	}{{Start, "/start", "120 data-live=alle"}, {Raeume, "/raeume", "120 data-live=seite"}, {Medien, "/medien", "120 data-live=seite"}, {Energie, "/energie", "60"},
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

// The pages' push (owner, 2026-10-24): /live streams each change of a
// thing Home Assistant reports, its entity id, while someone listens;
// sensors stay out; nobody signed in gets nothing; the last one gone ends
// the subscription.
func TestLiveStreamsWhatChanged(t *testing.T) {
	fake, hc := house(t)
	fake.Set("light.flur", "off", nil)
	l := &Live{HA: hc}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Remote-User", r.URL.Query().Get("wer"))
		l.ServeHTTP(w, r)
	}))
	defer srv.Close()
	if res, err := http.Get(srv.URL + "/live"); err != nil || res.StatusCode != http.StatusForbidden {
		t.Fatalf("nobody: %v %v", res, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/live?wer=anna", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%v %v", res, err)
	}
	if !fake.WaitSubscribed(1) {
		t.Fatal("no subscription to HA")
	}
	fake.Change("sensor.temp", "21.5", nil)
	fake.Change("light.flur", "on", nil)
	rd := bufio.NewReader(res.Body)
	var got []string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && len(got) < 2 {
		line, err := rd.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "event: ") || strings.HasPrefix(line, "data: ") {
			got = append(got, strings.TrimSpace(line))
		}
	}
	if strings.Join(got, "|") != "event: geaendert|data: light.flur" {
		t.Fatalf("stream %q", got)
	}
	cancel()
	res.Body.Close()
	if !fake.WaitSubscribedExactly(0) {
		t.Error("the subscription outlived its last listener")
	}
}
