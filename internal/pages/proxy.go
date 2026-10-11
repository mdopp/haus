package pages

import (
	"crypto/subtle"
	"net/http"
)

// RequireProxy lets a page request through only when it carries the secret
// Caddy adds as X-Haus-Proxy (ADR 0012 §3): Haus's port is on the host's
// loopback, which every host-network container shares, and Remote-User is
// just a header — without the secret nobody is anybody. /healthz is free.
func RequireProxy(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		got := r.Header.Get("X-Haus-Proxy")
		if secret == "" || subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
			http.Error(w, "nur über den Proxy der Box", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
