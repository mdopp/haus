package pages

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// haus#61, haus#67: every family page, Box too, says small at the top who
// is signed in, and nothing more there; Abmelden is under Mehr — on the
// Mehr page with the account, and in the list a wide window shows on the
// right. /abmelden sends to Authelia's logout beside Haus, and the login
// afterwards returns to Haus.
func TestAbmeldenUnderMehr(t *testing.T) {
	for _, groups := range []string{"family", "admins"} {
		_, body, _ := familyPage(t, Start, StartConfig{}, "GET", "/start", groups)
		mustContain(t, body, `<p class=konto><span class=wer>anna</span></p>`, `<div class=mehr>`, `Angemeldet als <span class=wer>anna</span>`, `<a href=/abmelden>`)
		before(t, body, "<div class=mehr>", "href=/abmelden")
		if top := body[strings.Index(body, "<p class=konto>"):]; strings.Contains(top[:strings.Index(top, "</p>")], "Abmelden") {
			t.Errorf("%s: Abmelden is still at the top", groups)
		}
		_, body, _ = familyPage(t, Mehr, StartConfig{}, "GET", "/mehr", groups)
		mustContain(t, body, "<h1>Mehr</h1>", "<h2 id=karte-konto>Konto</h2>", "Angemeldet als anna", `<a href="/abmelden">Abmelden`)
	}
	_, body, _ := familyPage(t, Elsewhere, StartConfig{}, "GET", "/gibtsnicht", "family")
	if !strings.Contains(body, `<a href=/abmelden>`) {
		t.Errorf("a 404 page has no way to Abmelden:\n%s", body)
	}
	req := httptest.NewRequest("GET", "/start", nil)
	req.Header.Set("Remote-User", `<x>`)
	if got := konto(req); strings.Contains(got, "<x>") {
		t.Errorf("the account is not escaped: %s", got)
	}
	if got := konto(httptest.NewRequest("GET", "/start", nil)); got != "" {
		t.Errorf("nobody signed in, yet %q", got)
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
