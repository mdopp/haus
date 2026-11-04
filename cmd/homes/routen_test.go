package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/napi"
)

// napiProben are a request for each /napi pattern Homes has ever served,
// and some it never did.
var napiProben = []string{
	"GET /napi/assistent", "GET /napi/app", "GET /napi/persona", "GET /napi/box/home", "GET /napi/box/services",
	"GET /napi/box/approvals", "GET /napi/box/upgrades", "POST /napi/box/services/homes/operate",
	"POST /napi/box/approvals/1/approve", "POST /napi/box/approvals/1/deny", "GET /napi/kann", "POST /napi/kann/anfordern",
	"GET /napi/whoami", "GET /napi/portal/events", "POST /napi/portal/watch", "POST /napi/ha/call", "GET /napi/portal/state",
	"GET /napi/portal/entity-history", "GET /napi/portal/active", "GET /napi/portal/camera/camera.tuer/snapshot",
	"GET /napi/portal/start/addable", "GET /napi/portal/favorites", "GET /napi/portal/energy", "GET /napi/defs/tool",
	"POST /napi/action-callback", "GET /napi/portal/tasks", "GET /napi/portal/contacts", "GET /napi/portal/documents",
	"GET /napi/portal/notes", "POST /napi/upload", "GET /napi/sicherung/stand", "POST /napi/sicherung/fehlend",
	"POST /napi/sicherung/uploads", "HEAD /napi/sicherung/uploads/x1", "PATCH /napi/sicherung/uploads/x1",
	"GET /napi/sicherung/schluessel", "GET /napi/sicherung/letzte", "GET /napi/sicherung/letzte/whatsapp/a/b.jpg",
	"HEAD /napi/sicherung/letzte/whatsapp/a/b.jpg", "POST /napi/merken", "POST /napi/kontakte", "GET /napi/gemeinsam/kontakte",
	"GET /napi/gemeinsam/kalender", "GET /napi/standort", "POST /napi/standort", "POST /napi/standort/import",
	"HEAD /napi/standort/import/x1", "PATCH /napi/standort/import/x1", "GET /napi/standort/import/x1",
	"GET /napi/gibtsnicht", "DELETE /napi/kann", "GET /napi/upload", "GET /napi/ha/call",
}

// routeTable is, for each probe, the pattern a mux answers it with ("-":
// none).
func routeTable(mux *http.ServeMux, proben []string) string {
	var b strings.Builder
	for _, p := range proben {
		method, path, _ := strings.Cut(p, " ")
		_, pattern := mux.Handler(httptest.NewRequest(method, path, nil))
		if pattern == "" {
			pattern = "-"
		}
		b.WriteString(p + " → " + pattern + "\n")
	}
	return b.String()
}

// routeBoxes are the boxes the route tables are taken on: every module,
// each module off in turn, fullSite and a box with nothing.
func routeBoxes(t *testing.T) ([]string, []site) {
	t.Helper()
	full, _, _ := fullSite(t)
	names, boxes := []string{"alles", "fullSite", "nichts"}, []site{leafSite(t), full, testSite(t)}
	for _, m := range katalog(leafSite(t)) {
		names, boxes = append(names, "ohne "+m.Name), append(boxes, leafSite(t))
	}
	for i := range boxes {
		aus := ""
		if n, ok := strings.CutPrefix(names[i], "ohne "); ok {
			aus = n
		}
		boxes[i], _ = switched(t, boxes[i], aus)
	}
	return names, boxes
}

// TestTheNapiRoutesAreAsTheyWere locks which pattern answers each /napi
// request on each box (homes#243: the route groups must not change it).
// UPDATE_GOLDEN=1 writes the file.
func TestTheNapiRoutesAreAsTheyWere(t *testing.T) {
	names, boxes := routeBoxes(t)
	var b strings.Builder
	for i, s := range boxes {
		b.WriteString("== " + names[i] + "\n" + routeTable(napi.Handler(s.napi).(*http.ServeMux), napiProben))
	}
	golden(t, "testdata/routen_napi.golden", b.String())
}

// golden compares got with the file, UPDATE_GOLDEN=1 writes it.
func golden(t *testing.T, file, got string) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") != "" {
		os.WriteFile(file, []byte(got), 0o644)
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs:\n%s", file, lineDiff(string(want), got))
	}
}
