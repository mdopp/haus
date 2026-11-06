package pages

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/laib"
)

const demoAuslastung = `{"at":"2026-11-06T11:00:00Z","uptime_s":1058335,"cpu":{"cores":16,"load":[0.52,0.41,0.3],"pct":12.5},
"mem":{"total":68719476736,"used":30064771072},"swap":{"total":4294967296,"used":0},
"disks":[{"role":"system","path":"/var","size":512000000000,"used":102400000000,"free":409600000000,"pct":20},
{"role":"data","path":"/mnt/data/stacks","size":4000000000000,"used":3600000000000,"free":400000000000,"pct":90}],
"raid":"laib diag, Probe raid","gpu":{"name":"NVIDIA GeForce RTX 3090","pct":87,"memUsed":21474836480,"memTotal":25769803776,"temp":66,"power":301,"powerLimit":350,"holder":"foundry","window":"exclusive","model":""},
"temps":[{"name":"cpu","c":54},{"name":"nvme0","c":61}],"missing":["Plattentemperatur: sda (kein drivetemp)"]}`

// mcpAnswer is a Laib that answers every tool call with text.
func mcpAnswer(t *testing.T, text string, isErr bool) *laib.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "homes"}
}

// owner 2026-11-06: the box's load, calm; a disk over 85 % in the warning
// colour, who holds the GPU, what Laib could not measure.
func TestAuslastungShowsTheValues(t *testing.T) {
	got := auslastungCard(context.Background(), mcpAnswer(t, demoAuslastung, false))
	for _, want := range []string{"<b>Daten</b>", `class="balken gelb"`, `class="balken "`, "3,3 TB von 3,6 TB belegt · 90 %", "Prozessor", "Last 0,52 · 0,41 · 0,30, 16 Kerne",
		"28 GB von 64 GB", `class="ring gelb" style="--p:87"`, "Gerade belegt von foundry (Fenster exclusive).", "301 von 350 Watt",
		"Platte nvme0</span><b class=\"k gelb\">61 °C", "Nicht messbar: Plattentemperatur: sda</p>", "läuft seit 12 Tagen"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "drivetemp") {
		t.Error("Laib's reason is shown to the family")
	}
	got = auslastungCard(context.Background(), mcpAnswer(t, `{"gpu":{"name":"X","holder":"","model":"qwen3.6-35b-a3b"}}`, false))
	if !strings.Contains(got, "Gerade frei für den Haushalt, Modell qwen3.6-35b-a3b.") {
		t.Errorf("household window: %s", got)
	}
}

// A Laib without the verb, or without the grant, says what is missing; one
// that fails says so plainly.
func TestAuslastungWithoutLaib(t *testing.T) {
	if got := auslastungCard(context.Background(), mcpAnswer(t, "homes may not auslastung: policy.yml limits it to status", true)); !strings.Contains(got, "Neue Standard-Verben") {
		t.Errorf("no grant: %s", got)
	}
	if got := auslastungCard(context.Background(), mcpAnswer(t, "kaputt", true)); !strings.Contains(got, saysLaibSilent) {
		t.Errorf("failed: %s", got)
	}
}

// The tab is Box's, admins only; its values come again in place, the page
// is not reloaded.
func TestAuslastungIsABoxTab(t *testing.T) {
	cfg := BoxConfig{Laib: mcpAnswer(t, demoAuslastung, false)}
	req := httptest.NewRequest("GET", auslastungPath, nil)
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Remote-Groups", "admins")
	rec := httptest.NewRecorder()
	BoxPage(cfg)(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(body, `<a href=/box/auslastung aria-current=page>Auslastung</a>`) || !strings.Contains(body, "id=box-auslastung data-teil=auslastung") ||
		!strings.Contains(body, "window.homesAuslastung") || strings.Contains(body, "location.reload") {
		t.Errorf("%d %s", rec.Code, body)
	}
	req = httptest.NewRequest("GET", BoxPartPath+"?was=auslastung", nil)
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Remote-Groups", "admins")
	rec = httptest.NewRecorder()
	BoxPage(cfg)(rec, req)
	if !strings.Contains(rec.Body.String(), `"box-auslastung"`) {
		t.Errorf("part: %s", rec.Body)
	}
	req = httptest.NewRequest("GET", auslastungPath, nil)
	req.Header.Set("Remote-User", "ben")
	rec = httptest.NewRecorder()
	BoxPage(cfg)(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("not an admin: %d", rec.Code)
	}
}
