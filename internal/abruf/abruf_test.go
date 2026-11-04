package abruf

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The owner, 2026-10-25: "es gibt zu wenig Infos warum es nicht geht" — a
// page that does not answer says why: the name, the way, the time.
func TestNoAnswerSaysWhy(t *testing.T) {
	ctx := context.Background()
	u, _ := url.Parse("https://gibt-es-nicht.invalid/rezept")
	_, _, err := Fetcher{}.Fetch(ctx, u)
	if !errors.Is(err, ErrNoAnswer) || !strings.Contains(err.Error(), "gibt-es-nicht.invalid") || !strings.Contains(err.Error(), "nicht") {
		t.Errorf("no such name: %v", err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	}))
	defer slow.Close()
	su, _ := url.Parse(slow.URL)
	_, _, err = Fetcher{AllowPrivate: true, Wait: 50 * time.Millisecond}.Fetch(ctx, su)
	if !errors.Is(err, ErrNoAnswer) || !strings.Contains(err.Error(), "nicht rechtzeitig") {
		t.Errorf("slow: %v", err)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	cu, _ := url.Parse(closed.URL)
	closed.Close()
	_, _, err = Fetcher{AllowPrivate: true}.Fetch(ctx, cu)
	if !errors.Is(err, ErrNoAnswer) || !strings.Contains(err.Error(), "kommt nicht zu 127.0.0.1 durch") {
		t.Errorf("no way: %v", err)
	}
}
