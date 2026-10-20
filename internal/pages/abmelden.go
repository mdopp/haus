package pages

import (
	"html"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Abmelden is the way out (haus#61, like Laib's own since laib#480):
// Authelia's logout, and the login afterwards returns to Haus, so a wrong
// account, or a shared tablet's month-long login, can switch from Haus.
// publicURL is Haus's own address (HAUS_PUBLIC_URL, https://haus.<domain>);
// Authelia is auth.<domain> beside it. Unset, the request's own host
// stands in. It logs nobody out itself: the GET only sends on.
func Abmelden(publicURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !showOnly(w, r, "Abmelden") {
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, logoutURL(publicURL, r.Host), http.StatusSeeOther)
	}
}

// logoutURL is https://auth.<domain>/logout?rd=<Haus>/: the domain is
// Haus's own host without its first label (haus.example.org → example.org).
func logoutURL(publicURL, reqHost string) string {
	host := reqHost
	if u, err := url.Parse(publicURL); err == nil && u.Host != "" {
		host = u.Host
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	domain := host
	if i := strings.IndexByte(host, '.'); i >= 0 {
		domain = host[i+1:]
	}
	back := "https://" + host + "/"
	return "https://auth." + domain + "/logout?rd=" + url.QueryEscape(back)
}

// konto is the top of every family page (haus#61): who is signed in, from
// Authelia's Remote-User, and Abmelden, the way to another account.
func konto(r *http.Request) string {
	who := ""
	if u := r.Header.Get("Remote-User"); u != "" {
		who = "<span>Angemeldet als <span class=wer>" + html.EscapeString(u) + "</span></span>"
	}
	return "<p class=konto>" + who + "<a href=/abmelden>Abmelden</a></p>"
}
