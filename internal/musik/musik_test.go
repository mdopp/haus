package musik

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// upload posts files (name → bytes) as the app's share target does.
func upload(t *testing.T, dir, bearer string, files ...[2]string) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("quelle", "teilen") // another part is skipped
	for _, f := range files {
		w, _ := mw.CreateFormFile("file", f[0])
		w.Write([]byte(f[1]))
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/napi/musik", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+bearer)
	rec := httptest.NewRecorder()
	Hochladen(rec, req, dir, func(b string) (string, bool) {
		return map[string]string{"anna-phone": "anna", "fremd": "Anna Müller"}[b], b != ""
	})
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// The share target (owner 2026-11-04: songs and ZIPs by Teilen → Homes): each file one
// upload at the inbox's top, beside one of the same name, the person's sidecar written
// first; no part left; what is not music, nothing of the request after it.
func TestHochladen(t *testing.T) {
	dir := t.TempDir()
	code, out := upload(t, dir, "anna-phone", [2]string{"Lied.mp3", "ID3"}, [2]string{"Album.ZIP", "PK"})
	if code != 200 || out["ok"] != true || len(out["dateien"].([]any)) != 2 {
		t.Fatalf("%d %v", code, out)
	}
	code, out = upload(t, dir, "anna-phone", [2]string{`C:\Musik\Lied.mp3`, "zwei"})
	if code != 200 || out["dateien"].([]any)[0] != "Lied (2).mp3" {
		t.Fatalf("beside: %d %v", code, out)
	}
	for name, want := range map[string]string{"Lied.mp3": "ID3", "Album.ZIP": "PK", "Lied (2).mp3": "zwei", "._Lied.mp3.von": "anna\n", "._Lied (2).mp3.von": "anna\n", "._Album.ZIP.von": "anna\n"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(b) != want {
			t.Errorf("%s = %q %v", name, b, err)
		}
	}
	upload(t, dir, "fremd", [2]string{"x.flac", "fLaC"}) // a name the importer does not take: no sidecar
	if _, err := os.Stat(filepath.Join(dir, "._x.flac.von")); err == nil {
		t.Error("sidecar for a name the importer does not take")
	}
	for _, bad := range [][2]string{{"notiz.txt", "x"}, {".versteckt.mp3", "x"}, {"leer.mp3", ""}} {
		code, out := upload(t, dir, "anna-phone", [2]string{"gut.ogg", "OggS"}, bad)
		if code != http.StatusBadRequest || out["ok"] != false || !strings.Contains(out["error"].(string), "„") || out["dateien"].([]any)[0] != "gut.ogg" {
			t.Errorf("%v → %d %v", bad, code, out)
		}
		os.Remove(filepath.Join(dir, "gut.ogg"))
	}
	if code, out := upload(t, dir, "anna-phone"); code != http.StatusBadRequest || out["error"] != "Es kam keine Musikdatei an." {
		t.Errorf("no file → %d %v", code, out)
	}
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		if strings.HasSuffix(e.Name(), ".part") || e.Name() == "notiz.txt" || e.Name() == "leer.mp3" {
			t.Errorf("left %s", e.Name())
		}
	}
	rec := httptest.NewRecorder()
	Hochladen(rec, httptest.NewRequest("POST", "/napi/musik", strings.NewReader("{}")), dir, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("not multipart → %d", rec.Code)
	}
}

func TestNameUndUploader(t *testing.T) {
	for in, want := range map[string]string{"Lied.mp3": "Lied.mp3", "a/b/Album.zip": "Album.zip", `x\y.wav`: "y.wav", "notiz.txt": "", ".x.mp3": "", "": "", "/": "", "a.rar": ""} {
		if got := Name(in); got != want {
			t.Errorf("Name(%q) = %q", in, got)
		}
	}
	for in, want := range map[string]string{"anna": "anna", "Erika": "erika", "a b": "", "": "", strings.Repeat("a", 65): "", "ben.k@x-y_z": "ben.k@x-y_z", "ä": ""} {
		if got := Uploader(in); got != want {
			t.Errorf("Uploader(%q) = %q", in, got)
		}
	}
}
