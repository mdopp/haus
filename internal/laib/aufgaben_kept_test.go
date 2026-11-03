package laib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A kept list is read once per AufgabenFresh; a tick through Homes forgets
// it, so the next look asks Laib again.
func TestKeptAufgabenForgetsOnATick(t *testing.T) {
	var lists atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params struct {
				Args map[string]any `json:"arguments"`
			} `json:"params"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := `{"message":"erledigt"}`
		if req.Params.Args["action"] == "list" {
			lists.Add(1)
			text = `{"offen":1,"aufgaben":[{"id":7,"titel":"T","fuer":"alle"}]}`
		}
		json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"content": []map[string]string{{"text": text}}}})
	}))
	t.Cleanup(srv.Close)
	c := &Client{URL: srv.URL, Token: "t"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 3; i++ {
		l, ok, err := c.KeptAufgaben(ctx, "anna")
		if !ok || err != nil || len(l.Aufgaben) != 1 {
			t.Fatalf("KeptAufgaben: %v %v %v", l, ok, err)
		}
	}
	if n := lists.Load(); n != 1 {
		t.Errorf("Laib asked %d times; kept, once", n)
	}
	if _, err := c.AufgabeErledigt(ctx, 7, "anna"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.KeptAufgaben(ctx, "anna"); !ok {
		t.Fatal("no list after the tick")
	}
	if n := lists.Load(); n != 2 {
		t.Errorf("after a tick Laib asked %d times in all; want 2", n)
	}
}
