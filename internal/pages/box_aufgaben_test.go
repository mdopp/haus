package pages

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// a Laib with Box-Aufgaben: list, erledigt and zurueckstellen as laib#667
// shapes them; what it was asked is kept
type aufgabenLaib struct {
	mu     sync.Mutex
	calls  []map[string]any
	tasks  []map[string]any
	noTool bool
}

func (a *aufgabenLaib) client(t *testing.T) *laib.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		a.mu.Lock()
		defer a.mu.Unlock()
		reply := func(v any, isErr bool) {
			text, ok := v.(string)
			if !ok {
				b, _ := json.Marshal(v)
				text = string(b)
			}
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}, "isError": isErr}})
		}
		if req.Params.Name != "laib_aufgabe" {
			reply("[]", false)
			return
		}
		if a.noTool {
			reply("haus may not aufgabe: policy.yml limits it to status, inbox", true)
			return
		}
		args := req.Params.Arguments
		a.calls = append(a.calls, args)
		find := func() map[string]any {
			for _, t := range a.tasks {
				if jsonString(t["id"]) == args["id"] {
					return t
				}
			}
			return nil
		}
		switch args["action"] {
		case "list":
			var open int
			for _, t := range a.tasks {
				if t["erledigt"] == nil {
					open++
				}
			}
			reply(map[string]any{"offen": open, "faellig": 1, "aufgaben": a.tasks}, false)
		case "erledigt":
			task := find()
			if task == nil {
				reply("Box-Aufgabe #"+args["id"].(string)+" gibt es nicht", true)
				return
			}
			task["erledigt"] = map[string]any{"am": time.Now().Format("2006-01-02"), "von": "haus:" + args["von"].(string)}
			reply(map[string]any{"message": "Box-Aufgabe #" + args["id"].(string) + " erledigt.", "aufgabe": task}, false)
		case "zurueckstellen":
			task := find()
			task["erinnern"] = args["erinnern"]
			reply(map[string]any{"message": "Box-Aufgabe #" + args["id"].(string) + " erinnert am " + args["erinnern"].(string) + ".", "aufgabe": task}, false)
		}
	}))
	t.Cleanup(srv.Close)
	return &laib.Client{URL: srv.URL, Token: "t"}
}

func jsonString(v any) string { b, _ := json.Marshal(v); return string(b) }

// haus#115: the Box-Aufgaben on the Box page — the due first, each with
// Erledigt and Später; a tick ends in the card saying laib's sentence and
// the task among the done ones; laib's refusal said as laib says it; a
// task laib closed itself said apart; admins only, from Haus's own page.
func TestBoxAufgaben(t *testing.T) {
	today := time.Now().Format("2006-01-02")
	past := time.Now().AddDate(0, 0, -2).Format("2006-01-02")
	future := time.Now().AddDate(0, 0, 9).Format("2006-01-02")
	fake := &aufgabenLaib{tasks: []map[string]any{
		{"id": 1, "titel": "Sicherung außer Haus einrichten", "warum": "Das Backup liegt nur auf der Datenplatte.", "seit": "2026-10-25", "von": "claude", "befund": "backup offsite"},
		{"id": 2, "titel": "Später erst", "seit": "2026-10-25", "von": "claude", "erinnern": future},
		{"id": 3, "titel": "Voice PE flashen", "seit": "2026-10-25", "von": "claude", "erinnern": past},
		{"id": 4, "titel": "/boot aufräumen", "seit": "2026-10-20", "von": "claude", "befund": "boot", "erledigt": map[string]any{"am": today, "von": "laib", "wie": "von Laib erledigt: Befund weg"}},
	}}
	cfg := BoxConfig{Laib: fake.client(t), LaibURL: "https://laib.box.test"}

	rec := boxAt(BoxPage(cfg), BoxPartPath+"?was=aufgaben", "admins")
	var parts map[string]string
	json.Unmarshal(rec.Body.Bytes(), &parts)
	card := parts["box-aufgaben"]
	mustContain(t, card, "<h2>Box-Aufgaben <span class=k>· 3 offen · <span class=gelb>1 fällig</span></span></h2>",
		"<li id=aufgabe-3><b>Voice PE flashen</b>", "fällig seit ", "erinnert am ", "Laib hakt sie selbst ab, wenn der Befund (backup offsite) weg ist",
		`<input type=hidden name=tun value=erledigt><input type=hidden name=id value=1><button data-laeuft="Hakt ab …">Erledigt</button>`,
		`<input type=date name=erinnern value=`+time.Now().AddDate(0, 0, 7).Format("2006-01-02"), "Zuletzt erledigt (1)", "Laib: erledigt, der Befund ist weg")
	if i3, i1, i2 := strings.Index(card, "aufgabe-3"), strings.Index(card, "aufgabe-1>"), strings.Index(card, "aufgabe-2>"); !(i3 < i1 && i1 < i2) {
		t.Errorf("order: due %d, none %d, deferred %d", i3, i1, i2)
	}

	h := Aufgaben(cfg)
	admin := map[string]string{"Remote-Groups": "admins"}
	rec = post(t, h, AufgabenPath, url.Values{"tun": {"erledigt"}, "id": {"3"}}, "anna", admin)
	if rec.Code != 200 {
		t.Fatalf("erledigt → %d %s", rec.Code, rec.Body)
	}
	mustContain(t, rec.Body.String(), `<div id=box-aufgaben data-teil=aufgaben aria-busy=false>`, "<div class=gut role=status><span class=punkt></span><p>Box-Aufgabe #3 erledigt.</p>",
		"Zuletzt erledigt (2)", "von anna")
	if got := fake.calls[len(fake.calls)-2]; got["action"] != "erledigt" || got["id"] != "3" || got["von"] != "anna" {
		t.Errorf("laib got %v", got)
	}

	rec = post(t, h, AufgabenPath, url.Values{"tun": {"spaeter"}, "id": {"1"}, "erinnern": {future}}, "anna", admin)
	mustContain(t, rec.Body.String(), "Box-Aufgabe #1 erinnert am "+future+".")
	for _, bad := range []url.Values{{"tun": {"spaeter"}, "id": {"1"}, "erinnern": {today}}, {"tun": {"spaeter"}, "id": {"1"}, "erinnern": {"1.12."}}, {"tun": {"erledigt"}}, {"tun": {"weg"}, "id": {"1"}}} {
		if rec := post(t, h, AufgabenPath, bad, "anna", admin); rec.Code != http.StatusBadRequest {
			t.Errorf("%v → %d", bad, rec.Code)
		}
	}
	// laib's refusal, as laib says it, the page as it was
	rec = post(t, h, AufgabenPath, url.Values{"tun": {"erledigt"}, "id": {"9"}}, "anna", admin)
	if rec.Code != http.StatusConflict || strings.TrimSpace(rec.Body.String()) != "Box-Aufgabe #9 gibt es nicht" {
		t.Errorf("refused → %d %q", rec.Code, rec.Body)
	}
	if rec := post(t, h, AufgabenPath, url.Values{"tun": {"erledigt"}, "id": {"1"}}, "anna", map[string]string{"Remote-Groups": "family"}); rec.Code == 200 {
		t.Error("a non-admin ticked one off")
	}
	if rec := post(t, h, AufgabenPath, url.Values{"tun": {"erledigt"}, "id": {"1"}}, "anna", map[string]string{"Remote-Groups": "admins", "Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}

	// laib without the verb for this hand yet: said what to do, no error page
	fake.noTool = true
	rec = boxAt(BoxPage(cfg), BoxPartPath+"?was=aufgaben", "admins")
	json.Unmarshal(rec.Body.Bytes(), &parts)
	mustContain(t, parts["box-aufgaben"], saysAufgabenNoTool)
	rec = post(t, h, AufgabenPath, url.Values{"tun": {"erledigt"}, "id": {"1"}}, "anna", admin)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), saysAufgabenNoTool) {
		t.Errorf("no tool → %d %q", rec.Code, rec.Body)
	}

	// laib away: a sentence, never a raw error; slow: within the wait
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
	}))
	defer down.Close()
	was := aufgabeWait
	aufgabeWait = 50 * time.Millisecond
	defer func() { aufgabeWait = was }()
	rec = post(t, Aufgaben(BoxConfig{Laib: &laib.Client{URL: down.URL, Token: "t"}}), AufgabenPath, url.Values{"tun": {"erledigt"}, "id": {"1"}}, "anna", admin)
	if rec.Code != http.StatusBadGateway || strings.TrimSpace(rec.Body.String()) != saysAufgabeDown {
		t.Errorf("slow → %d %q", rec.Code, rec.Body)
	}

	if got := linkAufgaben("gelb (zurückgestellt → Box-Aufgabe #3 „Spiegel“)"); got != `gelb (zurückgestellt → <a href="/box/aufgaben#aufgabe-3">Box-Aufgabe #3</a> „Spiegel“)` {
		t.Errorf("%q", got)
	}
}
