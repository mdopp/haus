package napi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// kann is GET /napi/kann (ADR 0001 §2): what this box can do, for the app,
// behind the paired device's bearer like every /napi path. The answer
// carries an ETag; the same one in If-None-Match is a 304. Without a list
// (a test's Config) it is a path Homes does not serve.
func (c Config) kann(w http.ResponseWriter, r *http.Request) {
	if c.Module == nil {
		noSuchPath(w, r)
		return
	}
	body, err := json.Marshal(c.Module.Kann())
	if err != nil {
		fail(w, err)
		return
	}
	sum := sha256.Sum256(body)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", tag)
	w.Header().Set("Cache-Control", "no-cache")
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(append(body, '\n'))
}
