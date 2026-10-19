package pages

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/devices"
)

// haus#6: the app opens <server>/pair-device in a Custom Tab after the person
// enters the address; Authelia logs them in, they confirm, and the redirect
// to cloud.dopp.haus://pair#token=haus_device_… hands the token to the app
// (the new app's scheme and prefix, haus#26). A GET mints nothing.
func TestPairDeviceFromThePhone(t *testing.T) {
	store, err := devices.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := PairDevice(store, StartConfig{})
	req := httptest.NewRequest("GET", "/pair-device", nil)
	req.Header.Set("Remote-User", "anna")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<form method=post") || len(store.List()) != 0 {
		t.Fatalf("GET shows a confirm form and mints nothing: %d %d\n%s", rec.Code, len(store.List()), rec.Body.String())
	}
	req = httptest.NewRequest("POST", "/pair-device", strings.NewReader("name=Pixel"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Remote-User", "anna")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	loc := rec.Header().Get("Location")
	if rec.Code != 302 || !strings.HasPrefix(loc, "cloud.dopp.haus://pair#token=haus_device_") || !strings.Contains(loc, "&id=") {
		t.Fatalf("POST redirects to the app with the token in the fragment: %d %q", rec.Code, loc)
	}
	list := store.List()
	if len(list) != 1 || list[0].UID != "anna" || list[0].Name != "Pixel" {
		t.Fatalf("one device for anna: %+v", list)
	}
	tok := strings.SplitN(strings.TrimPrefix(loc, "cloud.dopp.haus://pair#token="), "&", 2)[0]
	if d, ok := store.Lookup(tok); !ok || d.UID != "anna" {
		t.Fatal("the token in the redirect is the device's")
	}
	req = httptest.NewRequest("POST", "/pair-device", strings.NewReader("name=x"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 403 || len(store.List()) != 1 {
		t.Fatalf("without a Remote-User nothing is minted: %d", rec.Code)
	}
}

// haus#6: the Geräte page offers what works. The app registers only
// <scheme>://pair as a link; <scheme>://setup is QR content, so it is shown as
// text to put into a QR code, and the address to type into the app is named.
func TestGeraeteOffersWhatWorks(t *testing.T) {
	store, _ := devices.Open(t.TempDir())
	req := httptest.NewRequest("POST", "/box/geraete", strings.NewReader("name=Pixel"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Remote-User", "anna")
	req.Header.Set("Remote-Groups", "admins")
	rec := httptest.NewRecorder()
	Geraete(store, "https://haus.example", StartConfig{}).ServeHTTP(rec, req)
	body := rec.Body.String()
	if !strings.Contains(body, "cloud.dopp.haus://setup?server="+url.QueryEscape("https://haus.example")+"&amp;token=haus_device_") {
		t.Fatalf("the QR content is the new app's setup URI:\n%s", body)
	}
	if strings.Contains(body, `href="cloud.dopp.haus://setup`) {
		t.Fatalf("the setup URI is a link the app does not open:\n%s", body)
	}
	if !strings.Contains(body, "https://haus.example") || !strings.Contains(body, "QR") {
		t.Fatalf("the page names the address for the app and the QR content:\n%s", body)
	}
}
