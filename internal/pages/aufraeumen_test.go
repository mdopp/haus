package pages

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// a music folder with _unklar songs and an album, the plan beside Haus's
// data, and an inbox
func aufraeumenHouse(t *testing.T) (StartConfig, string, string) {
	t.Helper()
	music, inbox, data := t.TempDir(), t.TempDir(), t.TempDir()
	put := func(p, body string) {
		os.MkdirAll(filepath.Join(music, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(music, p), []byte(body), 0o644)
	}
	put("_unklar/Hardcore/Unbekanntes Album/Knight Rider Theme.mp3", "k")
	put("_unklar/Funky Chicken/Unbekanntes Album/Chicken Dance.mp3", "c")
	put("_unklar/Eric Clapton/Unbekanntes Album/Leila.mp3", "layla")
	put("_unklar/Babyshambles/Unbekanntes Album/01 Back From The Dead.mp3", "b1")
	put("_unklar/Babyshambles/Unbekanntes Album/02 Loyalty Song.mp3", "b2")
	put("_unklar/Bang Gang/Unbekanntes Album/I Like To Move It.mp3", "m")
	put("Alligatoah/Musik ist keine Loesung/02 - Musik ist keine Lösung.mp3", "gross")
	put("Alligatoah/Musik ist keine Loesung/04 - Musik ist keine Lösung.mp3", "k")
	put("Alligatoah/Musik ist keine Loesung/10 - Ein Problem mit Alkohol (Live in Kenia).mp3", "live")
	plan := musikPlan{Stand: "26.10.2026", Unklar: []planUnklar{
		{Pfad: "_unklar/Hardcore/Unbekanntes Album/Knight Rider Theme.mp3", Gruppe: 1, Grund: "Spaß-Remix"},
		{Pfad: "_unklar/Funky Chicken/Unbekanntes Album/Chicken Dance.mp3", Gruppe: 1, Grund: "Spaß-Remix"},
		{Pfad: "_unklar/Eric Clapton/Unbekanntes Album/Leila.mp3", Gruppe: 2, Grund: "Titel falsch geschrieben", Name: "Eric Clapton - Layla"},
		{Pfad: "_unklar/Babyshambles/Unbekanntes Album", Gruppe: 2, Grund: "Album ohne Tags", Name: "Babyshambles - Down in Albion", Ordner: true},
		{Pfad: "_unklar/Bang Gang/Unbekanntes Album/I Like To Move It.mp3", Gruppe: 3, Grund: "falscher Interpret", Wunsch: "Reel 2 Real – I Like To Move It"},
	}, Alben: []planAlbum{{Album: "Alligatoah/Musik ist keine Loesung", Titel: 3, Befunde: []planBefund{
		{Datei: "04 - Musik ist keine Lösung.mp3", Art: "doppelt", Behalten: "02 - Musik ist keine Lösung.mp3"},
		{Datei: "10 - Ein Problem mit Alkohol (Live in Kenia).mp3", Art: "live"},
	}}}}
	b, _ := json.Marshal(plan)
	os.WriteFile(filepath.Join(data, musikPlanFile), b, 0o600)
	return StartConfig{MusicDir: music, MusicInbox: inbox, DataDir: data}, music, inbox
}

var admin = map[string]string{"Remote-Groups": "admins"}

func aufraeumenDo(t *testing.T, cfg StartConfig, form url.Values) (int, string) {
	t.Helper()
	rec := post(t, MusikAufraeumen(cfg), MusikAufraeumenPath, form, "mdopp", admin)
	return rec.Code, rec.Body.String()
}

func gone(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Stat(p); err == nil {
		t.Errorf("%s is still there", p)
	}
}

// owner 2026-10-26: the plan's groups, each step pressed, deleting asked first
func TestMusikAufraeumenShowsThePlan(t *testing.T) {
	cfg, _, _ := aufraeumenHouse(t)
	_, body, _ := familyPage(t, MusikAufraeumen, cfg, "GET", MusikAufraeumenPath, "admins")
	mustContain(t, body, "Vorschlag: löschen · 2", "Zurück in den Musik-Eingang · 2", "Prüfen oder ersetzen · 1", "Alben · 1",
		`data-frage="Alle 2 Titel dieser Gruppe endgültig löschen?"`, "→ „Eric Clapton - Layla“", "eigentlich „Reel 2 Real – I Like To Move It“",
		"Auf die Wunschliste", "Alle 1 doppelten Titel löschen", "Live-Titel neu einsortieren", "(bleibt: 02 - Musik ist keine Lösung.mp3)", "Passt so")
	if code, body, _ := familyPage(t, MusikAufraeumen, cfg, "GET", MusikAufraeumenPath, "family"); code != http.StatusForbidden || !strings.Contains(body, "nur Admins") {
		t.Errorf("family → %d", code)
	}
	_, body, _ = familyPage(t, Musik, cfg, "GET", MusikPath, "admins")
	mustContain(t, body, `<a href="/musik/aufraeumen">Musik aufräumen</a>`)
	_, body, _ = familyPage(t, Musik, cfg, "GET", MusikPath, "family")
	mustNotContain(t, body, "/musik/aufraeumen")
}

func TestMusikAufraeumenWithoutPlanOrMusic(t *testing.T) {
	cfg := StartConfig{MusicDir: t.TempDir(), DataDir: t.TempDir()}
	_, body, _ := familyPage(t, MusikAufraeumen, cfg, "GET", MusikAufraeumenPath, "admins")
	mustContain(t, body, "Noch kein Vorschlag")
	cfg, _, _ = aufraeumenHouse(t)
	cfg.MusicDir = filepath.Join(cfg.MusicDir, "fehlt")
	_, body, _ = familyPage(t, MusikAufraeumen, cfg, "GET", MusikAufraeumenPath, "admins")
	mustContain(t, body, "Haus sieht die Musiksammlung gerade nicht")
}

func TestMusikAufraeumenDeletes(t *testing.T) {
	cfg, music, _ := aufraeumenHouse(t)
	knight := "_unklar/Hardcore/Unbekanntes Album/Knight Rider Theme.mp3"
	code, body := aufraeumenDo(t, cfg, url.Values{"tun": {"loeschen"}, "pfad": {knight}})
	if code != http.StatusOK {
		t.Fatalf("→ %d %s", code, body)
	}
	mustContain(t, body, "„Knight Rider Theme.mp3“ ist gelöscht.", "Vorschlag: löschen · 1")
	gone(t, filepath.Join(music, knight))
	if code, _ := aufraeumenDo(t, cfg, url.Values{"tun": {"loeschen"}, "pfad": {knight}}); code != http.StatusConflict {
		t.Errorf("twice → %d", code)
	}
	for _, bad := range []string{"Alligatoah/Musik ist keine Loesung/02 - Musik ist keine Lösung.mp3", "../x", ""} {
		if code, _ := aufraeumenDo(t, cfg, url.Values{"tun": {"loeschen"}, "pfad": {bad}}); code != http.StatusBadRequest {
			t.Errorf("%q not in the plan → %d", bad, code)
		}
	}
	if rec := post(t, MusikAufraeumen(cfg), MusikAufraeumenPath, url.Values{"tun": {"gruppe-loeschen"}, "gruppe": {"1"}}, "mdopp",
		map[string]string{"Remote-Groups": "admins", "Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
	if rec := post(t, MusikAufraeumen(cfg), MusikAufraeumenPath, url.Values{"tun": {"gruppe-loeschen"}, "gruppe": {"1"}}, "anna", nil); rec.Code != http.StatusForbidden {
		t.Errorf("not an admin → %d", rec.Code)
	}
	_, body = aufraeumenDo(t, cfg, url.Values{"tun": {"gruppe-loeschen"}, "gruppe": {"1"}})
	mustContain(t, body, "1 Titel gelöscht.")
	mustNotContain(t, body, "Vorschlag: löschen")
	gone(t, filepath.Join(music, "_unklar/Funky Chicken/Unbekanntes Album/Chicken Dance.mp3"))
	if _, err := os.Stat(filepath.Join(music, "_unklar/Eric Clapton/Unbekanntes Album/Leila.mp3")); err != nil {
		t.Error("a song of another group went")
	}
}

func TestMusikAufraeumenPutsBackUnderTheRightName(t *testing.T) {
	cfg, music, inbox := aufraeumenHouse(t)
	_, body := aufraeumenDo(t, cfg, url.Values{"tun": {"eingang"}, "pfad": {"_unklar/Eric Clapton/Unbekanntes Album/Leila.mp3"}})
	mustContain(t, body, "„Eric Clapton - Layla.mp3“ liegt im Musik-Eingang.")
	if b, _ := os.ReadFile(filepath.Join(inbox, "Eric Clapton - Layla.mp3")); string(b) != "layla" {
		t.Errorf("in the inbox: %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(inbox, "._Eric Clapton - Layla.mp3.von")); string(b) != "mdopp\n" {
		t.Errorf("uploader: %q", b)
	}
	gone(t, filepath.Join(music, "_unklar/Eric Clapton/Unbekanntes Album/Leila.mp3"))
	if code, _ := aufraeumenDo(t, cfg, url.Values{"tun": {"eingang"}, "pfad": {"_unklar/Bang Gang/Unbekanntes Album/I Like To Move It.mp3"}}); code != http.StatusBadRequest {
		t.Errorf("group 3 back → %d", code)
	}
	_, body = aufraeumenDo(t, cfg, url.Values{"tun": {"gruppe-eingang"}, "gruppe": {"2"}})
	mustContain(t, body, "1 Titel liegt wieder im Musik-Eingang")
	if b, _ := os.ReadFile(filepath.Join(inbox, "Babyshambles - Down in Albion", "02 Loyalty Song.mp3")); string(b) != "b2" {
		t.Errorf("the album folder: %q", b)
	}
	gone(t, filepath.Join(music, "_unklar/Babyshambles/Unbekanntes Album"))
	entries, _ := os.ReadDir(inbox)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("a part stayed: %s", e.Name())
		}
	}
}

func TestMusikAufraeumenKeepsAndWishes(t *testing.T) {
	cfg, music, _ := aufraeumenHouse(t)
	move := "_unklar/Bang Gang/Unbekanntes Album/I Like To Move It.mp3"
	_, body := aufraeumenDo(t, cfg, url.Values{"tun": {"wunsch"}, "pfad": {move}})
	mustContain(t, body, "Das ging nicht: Die Musik-Wunschliste ist nicht eingerichtet.", "Prüfen oder ersetzen · 1")
	_, body = aufraeumenDo(t, cfg, url.Values{"tun": {"behalten"}, "pfad": {move}})
	mustContain(t, body, "„I Like To Move It.mp3“ bleibt, wo es ist.")
	mustNotContain(t, body, "Prüfen oder ersetzen")
	if _, err := os.Stat(filepath.Join(music, move)); err != nil {
		t.Error("kept, but gone")
	}
}

func TestMusikAufraeumenAlbums(t *testing.T) {
	cfg, music, inbox := aufraeumenHouse(t)
	alb := "Alligatoah/Musik ist keine Loesung"
	_, body := aufraeumenDo(t, cfg, url.Values{"tun": {"album-doppelt"}, "album": {alb}})
	mustContain(t, body, "1 Titel gelöscht.")
	gone(t, filepath.Join(music, alb, "04 - Musik ist keine Lösung.mp3"))
	if _, err := os.Stat(filepath.Join(music, alb, "02 - Musik ist keine Lösung.mp3")); err != nil {
		t.Error("the copy that stays went")
	}
	_, body = aufraeumenDo(t, cfg, url.Values{"tun": {"album-live"}, "album": {alb}})
	mustContain(t, body, "1 Live-Titel liegt im Musik-Eingang")
	if _, err := os.Stat(filepath.Join(inbox, "10 - Ein Problem mit Alkohol (Live in Kenia).mp3")); err != nil {
		t.Error("the live song is not in the inbox")
	}
	mustNotContain(t, body, "Alben ·")
	if code, _ := aufraeumenDo(t, cfg, url.Values{"tun": {"album-doppelt"}, "album": {"Andere/Album"}}); code != http.StatusBadRequest {
		t.Errorf("an album not in the plan → %d", code)
	}
}

// a double whose kept copy is gone is not deleted: nothing of a song is lost
func TestMusikAufraeumenKeepsTheLastCopy(t *testing.T) {
	cfg, music, _ := aufraeumenHouse(t)
	alb := "Alligatoah/Musik ist keine Loesung"
	os.Remove(filepath.Join(music, alb, "02 - Musik ist keine Lösung.mp3"))
	_, body := aufraeumenDo(t, cfg, url.Values{"tun": {"alle-doppelten"}})
	mustContain(t, body, "0 Titel gelöscht.")
	if _, err := os.Stat(filepath.Join(music, alb, "04 - Musik ist keine Lösung.mp3")); err != nil {
		t.Error("the last copy went")
	}
	_, body = aufraeumenDo(t, cfg, url.Values{"tun": {"album-passt"}, "album": {alb}})
	mustContain(t, body, "„Alligatoah/Musik ist keine Loesung“ bleibt, wie es ist.")
	mustNotContain(t, body, "Alben ·")
}
