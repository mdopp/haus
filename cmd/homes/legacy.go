package main

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// legacyLabel is legacy: the program's old address, haus.<domain>, which the
// box still routes here after the rename (laib#1021). Ends when every phone
// runs the Homes app (laib#1021 step 9): the route becomes a redirectFrom.
const legacyLabel = "haus"

// legacyHost sends a page asked for under the old name, haus.<domain> (the
// domain of publicURL), to the same path on publicURL with a 308. The old
// Haus app's calls stay where they are: it calls /napi/… with its device
// token, and OkHttp drops Authorization on a redirect to another host, so a
// redirect would log every old phone out. Anything else — inside the box
// (host.containers.internal, 127.0.0.1: the Bote's /mcp/haushalt, the health
// check) or under the new name — is served as it is.
func legacyHost(publicURL string, next http.Handler) http.Handler {
	old, target := oldHost(publicURL)
	if old == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(hostOnly(r.Host), old) && !oldAppCalls(r.URL.Path) {
			http.Redirect(w, r, target+r.URL.RequestURI(), http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// oldAppCalls says whether the old app calls path with its device token
// (haus-android's data/Napi.kt: every endpoint is under /napi/; its pages,
// /start, /pair-device and the rest, open in the browser and may move).
func oldAppCalls(path string) bool { return strings.HasPrefix(path, "/napi/") }

// oldHost is haus.<domain> for publicURL https://homes.<domain>, and the
// origin to redirect to; empty when publicURL names no such host.
func oldHost(publicURL string) (old, target string) {
	u, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return "", ""
	}
	_, domain, ok := strings.Cut(u.Hostname(), ".")
	if !ok || domain == "" || net.ParseIP(u.Hostname()) != nil {
		return "", ""
	}
	old = legacyLabel + "." + strings.ToLower(domain)
	if strings.EqualFold(u.Hostname(), old) { // still under the old name: nothing to send anywhere
		return "", ""
	}
	return old, u.Scheme + "://" + u.Host
}

func hostOnly(h string) string {
	if host, _, err := net.SplitHostPort(h); err == nil {
		return host
	}
	return h
}
