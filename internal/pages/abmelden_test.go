package pages

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// haus#61: every family page, Box too, says who is signed in and has
// Abmelden; /abmelden sends to Authelia's logout beside Haus, and the
// login afterwards returns to Haus.
func TestAbmeldenOnEveryPage(t *testing.T) {
	for _, groups := range []string{"family", "admins"} {
		_, body, _ := familyPage(t, Start, StartConfig{}, "GET", "/start", groups)
		if !strings.Contains(body, `Angemeldet als <span class=wer>anna</span>`) || !strings.Contains(body, `<a href=/abmelden>Abmelden</a>`) {
			t.Errorf("%s: Start has no account line:\n%s", groups, body)
		}
	}
	_, body, _ := familyPage(t, Elsewhere, StartConfig{}, "GET", "/gibtsnicht", "family")
	if !strings.Contains(body, `<a href=/abmelden>Abmelden</a>`) {
		t.Errorf("a 404 page has no Abmelden:\n%s", body)
	}
	req := httptest.NewRequest("GET", "/start", nil)
	req.Header.Set("Remote-User", `<x>`)
	if got := konto(req); strings.Contains(got, "<x>") {
		t.Errorf("the account is not escaped: %s", got)
	}
}

func TestAbmeldenSendsToAutheliasLogout(t *testing.T) {
	for _, c := range []struct{ public, host, want string }{
		{"https://haus.example.org", "", "https://auth.example.org/logout?rd=https%3A%2F%2Fhaus.example.org%2F"},
		{"https://haus.example.org/", "ignored.test", "https://auth.example.org/logout?rd=https%3A%2F%2Fhaus.example.org%2F"},
		{"", "haus.dopp.cloud:443", "https://auth.dopp.cloud/logout?rd=https%3A%2F%2Fhaus.dopp.cloud%2F"},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/abmelden", nil)
		req.Host = c.host
		Abmelden(c.public)(rec, req)
		if rec.Code != 303 || rec.Header().Get("Location") != c.want {
			t.Errorf("%q/%q: %d %q, want %q", c.public, c.host, rec.Code, rec.Header().Get("Location"), c.want)
		}
	}
	rec := httptest.NewRecorder()
	Abmelden("https://haus.example.org")(rec, httptest.NewRequest("POST", "/abmelden", nil))
	if rec.Code != 405 {
		t.Errorf("POST /abmelden: %d, want 405", rec.Code)
	}
}
