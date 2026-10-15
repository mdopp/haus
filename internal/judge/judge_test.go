package judge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/judge/proxyfake"
)

var bg = context.Background()

var two = []Item{{"Kaminkehrer", "Herr Brandl kommt im Oktober."}, {"Gastherme", "Wartung im September, Firma Huber."}}

func client(p *proxyfake.Server) *Client {
	return &Client{URL: p.Base(), Model: "gemma-4-e4b", Key: "model-key"}
}

// The one call (haus#13): only after the lease says the house model is
// loaded and ready, for that model, temperature 0, the eval's German
// instruction, every item numbered with its title and passage, the answer
// held to {"ids":[…]} with numbers 1…n; the key as a bearer on the chat,
// none on the lease.
func TestAsksTheLoadedHouseModelOnce(t *testing.T) {
	p := proxyfake.New("gemma-4-e4b")
	defer p.Close()
	p.Content = `{"ids":[2]}`
	ids, err := client(p).Pick(bg, "Wer wartet die Heizung?", two)
	if err != nil || !reflect.DeepEqual(ids, []int{1}) {
		t.Fatalf("%v %v", ids, err)
	}
	seen := p.Seen()
	if len(seen) != 2 || seen[0].Method != "GET" || seen[0].Path != "/api/model-lease" || seen[0].Auth != "" ||
		seen[1].Method != "POST" || seen[1].Path != "/v1/chat/completions" || seen[1].Auth != "Bearer model-key" {
		t.Fatalf("requests: %+v", seen)
	}
	var body struct {
		Model          string  `json:"model"`
		Temperature    float64 `json:"temperature"`
		Messages       []struct{ Role, Content string }
		ResponseFormat struct {
			Type       string `json:"type"`
			JSONSchema struct {
				Schema struct {
					Required   []string `json:"required"`
					Properties struct {
						IDs struct {
							Items struct {
								Type    string `json:"type"`
								Minimum int    `json:"minimum"`
								Maximum int    `json:"maximum"`
							} `json:"items"`
						} `json:"ids"`
					} `json:"properties"`
				} `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	raw := map[string]any{}
	json.Unmarshal(seen[1].Body, &raw)
	if err := json.Unmarshal(seen[1].Body, &body); err != nil {
		t.Fatal(err)
	}
	if body.Model != "gemma-4-e4b" || raw["temperature"] != float64(0) || body.ResponseFormat.Type != "json_schema" {
		t.Fatalf("body: %s", seen[1].Body)
	}
	if it := body.ResponseFormat.JSONSchema.Schema.Properties.IDs.Items; it.Type != "integer" || it.Minimum != 1 || it.Maximum != 2 ||
		!reflect.DeepEqual(body.ResponseFormat.JSONSchema.Schema.Required, []string{"ids"}) {
		t.Fatalf("schema: %s", seen[1].Body)
	}
	if len(body.Messages) != 2 || body.Messages[0].Role != "system" || !strings.HasPrefix(body.Messages[0].Content, "Du prüfst Suchtreffer aus den Notizen einer Familie.") {
		t.Fatalf("instruction: %+v", body.Messages)
	}
	want := "Frage: Wer wartet die Heizung?\n\nNotizen:\n[1] Kaminkehrer — Herr Brandl kommt im Oktober.\n[2] Gastherme — Wartung im September, Firma Huber.\n\n" +
		"Welche Notizen beantworten die Frage? Antworte als JSON: {\"ids\": [Nummern]}"
	if body.Messages[1].Role != "user" || body.Messages[1].Content != want {
		t.Fatalf("prompt:\n%s", body.Messages[1].Content)
	}
}

// The lease decides whether to ask at all: the house model ready in a
// window, or no window at all with the house model as the default (nobody
// holds the card; asking takes it from no one, and a model still loading
// only runs out the budget). Another model in the window, the house model
// still loading, or no window with another default is ErrNotLoaded, and
// no chat is sent: a chat could make the card load or swap.
func TestOnlyWhileTheHouseModelIsLoaded(t *testing.T) {
	for _, l := range []proxyfake.Lease{
		{State: "ready", Model: "shared", Alias: "gemma-4-12b", Holder: "chronicle"},
		{State: "ready", Model: "full", Alias: "qwen3.6-35b-a3b", Holder: "pi"},
		{State: "preparing", Model: "shared", Alias: "gemma-4-e4b", Holder: "chronicle"},
		{State: "none", Alias: "gemma-4-12b"},
		{State: "", Alias: "gemma-4-e4b"},
		{State: "ready", Model: "exclusive", Holder: "maintenance"},
	} {
		p := proxyfake.New("gemma-4-e4b")
		p.Lease = l
		if ids, err := client(p).Pick(bg, "q", two); !errors.Is(err, ErrNotLoaded) || ids != nil {
			t.Errorf("%+v: %v %v", l, ids, err)
		}
		if n := len(p.Chats()); n != 0 {
			t.Errorf("%+v: %d chats", l, n)
		}
		p.Close()
	}
}

// No window held, the house model the default: nobody holds the card, so
// Haus asks, once, for the house model.
func TestAsksWhenNobodyHoldsTheCard(t *testing.T) {
	p := proxyfake.New("gemma-4-e4b")
	defer p.Close()
	p.Lease = proxyfake.Lease{State: "none", Alias: "gemma-4-e4b"}
	p.Content = `{"ids":[1]}`
	if ids, err := client(p).Pick(bg, "q", two); err != nil || !reflect.DeepEqual(ids, []int{0}) {
		t.Fatalf("%v %v", ids, err)
	}
	if n := len(p.Chats()); n != 1 {
		t.Fatalf("%d chats", n)
	}
}

// Anything but a valid answer is an error, so the caller keeps its hits:
// a refusal, a broken proxy, a lease or an answer that is not JSON, ids
// that are not there, no answer in time.
func TestWhatIsNotAnAnswer(t *testing.T) {
	for name, set := range map[string]func(*proxyfake.Server){
		"409":          func(p *proxyfake.Server) { p.Status = http.StatusConflict },
		"500":          func(p *proxyfake.Server) { p.Status = http.StatusInternalServerError },
		"prose":        func(p *proxyfake.Server) { p.Content = "Notiz 2 passt." },
		"no ids":       func(p *proxyfake.Server) { p.Content = `{"auswahl":[2]}` },
		"ids null":     func(p *proxyfake.Server) { p.Content = `{"ids":null}` },
		"ids text":     func(p *proxyfake.Server) { p.Content = `{"ids":"2"}` },
		"zero":         func(p *proxyfake.Server) { p.Content = `{"ids":[0]}` },
		"past the end": func(p *proxyfake.Server) { p.Content = `{"ids":[1,3]}` },
		"a fraction":   func(p *proxyfake.Server) { p.Content = `{"ids":[1.5]}` },
		"slow":         func(p *proxyfake.Server) { p.Hang = 3 * time.Second },
	} {
		p := proxyfake.New("gemma-4-e4b")
		set(p)
		ctx, cancel := context.WithTimeout(bg, 200*time.Millisecond)
		start := time.Now()
		ids, err := client(p).Pick(ctx, "q", two)
		cancel()
		if err == nil || ids != nil {
			t.Errorf("%s: %v %v, want an error", name, ids, err)
		}
		if took := time.Since(start); took > time.Second {
			t.Errorf("%s: took %v", name, took)
		}
		p.Close()
	}
	// a proxy that answers no chat completion, or a lease that is not JSON
	for _, body := range []string{`{"choices":[]}`, `nicht json`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.Write([]byte(`{"state":"ready","alias":"gemma-4-e4b"}`))
				return
			}
			w.Write([]byte(body))
		}))
		if _, err := (&Client{URL: s.URL + "/v1", Model: "gemma-4-e4b", Key: "k"}).Pick(bg, "q", two); err == nil {
			t.Errorf("%s: want an error", body)
		}
		s.Close()
	}
	lease := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`kaputt`)) }))
	defer lease.Close()
	if _, err := (&Client{URL: lease.URL + "/v1", Model: "gemma-4-e4b", Key: "k"}).Pick(bg, "q", two); err == nil || errors.Is(err, ErrNotLoaded) {
		t.Errorf("a lease that is not JSON: %v", err)
	}
}

// An empty list is the model's answer, not an error; ids come back in
// the items' order, each once.
func TestTheAnswer(t *testing.T) {
	p := proxyfake.New("gemma-4-e4b")
	defer p.Close()
	for content, want := range map[string][]int{`{"ids":[]}`: {}, `{"ids":[2,1,2]}`: {0, 1}, `{"ids":[1]}`: {0}} {
		p.Content = content
		if ids, err := client(p).Pick(bg, "q", two); err != nil || !reflect.DeepEqual(ids, want) {
			t.Errorf("%s: %v %v, want %v", content, ids, err, want)
		}
	}
}

// Haus never takes the lease: whatever happens, no POST or DELETE of it.
func TestNeverTakesTheLease(t *testing.T) {
	p := proxyfake.New("gemma-4-e4b")
	defer p.Close()
	for _, set := range []func(){
		func() {},
		func() { p.Lease.Alias = "gemma-4-12b" },
		func() { p.Lease.State = "none" },
		func() {
			p.Lease = proxyfake.Lease{State: "ready", Model: "shared", Alias: "gemma-4-e4b"}
			p.Status = http.StatusConflict
		},
	} {
		set()
		client(p).Pick(bg, "q", two)
	}
	for _, r := range p.Seen() {
		if r.Path == "/api/model-lease" && r.Method != http.MethodGet {
			t.Fatalf("%s %s", r.Method, r.Path)
		}
		if r.Path != "/api/model-lease" && r.Path != "/v1/chat/completions" {
			t.Fatalf("%s %s", r.Method, r.Path)
		}
	}
}

// The three come together, or there is no tidy-up; never an error.
func TestFromEnv(t *testing.T) {
	set := func(u, m, k string) *Client {
		t.Setenv("HAUS_LLM_URL", u)
		t.Setenv("HAUS_LLM_MODEL", m)
		t.Setenv("HAUS_LLM_KEY", k)
		return FromEnv()
	}
	if c := set("", "", ""); c != nil {
		t.Fatalf("none set: %+v", c)
	}
	c := set("http://host.containers.internal:11435/v1/", "gemma-4-e4b", "k")
	if c == nil || c.URL != "http://host.containers.internal:11435/v1" || c.Model != "gemma-4-e4b" || c.Key != "k" {
		t.Fatalf("all set: %+v", c)
	}
	for _, v := range [][3]string{
		{"http://h:11435/v1", "gemma-4-e4b", ""},
		{"http://h:11435/v1", "", "k"},
		{"", "gemma-4-e4b", "k"},
		{"h:11435/v1", "gemma-4-e4b", "k"},
		{"ftp://h/v1", "gemma-4-e4b", "k"},
	} {
		if c := set(v[0], v[1], v[2]); c != nil {
			t.Errorf("%v: %+v", v, c)
		}
	}
}
