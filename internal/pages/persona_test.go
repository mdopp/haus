package pages

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/devices"
	"github.com/mdopp/haus/internal/persona"
)

// laib#129: the family sees the house persona: name in the title and the
// header, its greeting, its icon and colours. Internally it stays Haus.
func TestPagesWearTheHousePersona(t *testing.T) {
	House = persona.Persona{Display: "Joshua", Greeting: map[string]string{"de": "Hallo, hier ist Joshua."},
		Colors: map[string]string{"accent": "#3ddc84", "ground": "#0c120e"}, Icon: []byte("<svg/>")}
	defer func() { House = persona.Default() }()
	store, _ := devices.Open(t.TempDir())
	req := httptest.NewRequest("GET", "/box/geraete", nil)
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Remote-Groups", "admins")
	rec := httptest.NewRecorder()
	Geraete(store, "https://haus.example").ServeHTTP(rec, req)
	body := rec.Body.String()
	for _, want := range []string{"<title>Geräte · Joshua</title>", "Hallo, hier ist Joshua.", "#3ddc84", "#0c120e", `src="/persona/icon.svg"`} {
		if !strings.Contains(body, want) {
			t.Errorf("%q missing:\n%s", want, body)
		}
	}
	if strings.Contains(body, "· Haus</title>") {
		t.Fatalf("the title still says Haus:\n%s", body)
	}
	rec = httptest.NewRecorder()
	Icon().ServeHTTP(rec, httptest.NewRequest("GET", "/persona/icon.svg", nil))
	if rec.Body.String() != "<svg/>" || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("icon: %q %q", rec.Header().Get("Content-Type"), rec.Body.String())
	}
}
