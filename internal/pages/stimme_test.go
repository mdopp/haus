package pages

import (
	"bytes"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/persona"
)

// a take as the page sends it: rate Hz, ch channels, sec seconds of a tone
// at amp (0: silence)
func take(rate, ch int, sec, amp float64) []byte {
	n := int(float64(rate) * sec)
	var pcm bytes.Buffer
	for i := 0; i < n; i++ {
		v := int16(math.Max(-32768, math.Min(32767, amp*32767*math.Sin(2*math.Pi*220*float64(i)/float64(rate))))) // over 1: clipped
		for c := 0; c < ch; c++ {
			binary.Write(&pcm, binary.LittleEndian, v)
		}
	}
	var b bytes.Buffer
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, uint32(36+pcm.Len()))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(ch), uint32(rate), uint32(rate * ch * 2), uint16(ch * 2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, uint32(pcm.Len()))
	b.Write(pcm.Bytes())
	return b.Bytes()
}

func joshua(t *testing.T) {
	t.Helper()
	old := House
	House = persona.Persona{Display: "Joshua", Phrase: "Hey Joshua", Wakeword: "hey_joshua"}
	t.Cleanup(func() { House = old })
}

func stimmePost(t *testing.T, cfg StartConfig, body []byte, user string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", StimmePath, bytes.NewReader(body))
	req.Host = "haus.box.test"
	req.Header.Set("Content-Type", "audio/wav")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if user != "" {
		req.Header.Set("Remote-User", user)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	Stimme(cfg)(rec, req)
	return rec
}

// owner 2026-10-27 (laib#825/#828): each person says the wake phrase a few
// times; Haus keeps each take as laib takes it, in the person's name, 0600
func TestStimmeKeepsATake(t *testing.T) {
	joshua(t)
	dir := t.TempDir()
	at := time.Date(2026, 10, 27, 19, 5, 9, 0, time.UTC)
	cfg := StartConfig{WakeRecordings: dir, Now: func() time.Time { return at }}

	code, body, _ := familyPage(t, Stimme, cfg, "GET", StimmePath, "family")
	if code != http.StatusOK {
		t.Fatalf("→ %d", code)
	}
	mustContain(t, body, "Sag 5-mal „Hey Joshua“", "Noch keine Aufnahme von dir.", `data-phrase="Hey Joshua">Aufnahme 1 von 5</button>`, "getUserMedia", "16000")
	_, body, _ = familyPage(t, Mehr, cfg, "GET", mehrPath, "family") // haus#188: a task under Einrichtung, not a page of Mehr
	mustContain(t, body, "<h2 id=karte-einrichtung>Einrichtung</h2>", `<a href="/stimme">Joshua deine Stimme beibringen</a>`)

	rec := stimmePost(t, cfg, take(16000, 1, 1.5, 0.3), "Anna", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("a good take → %d %s", rec.Code, rec.Body)
	}
	mustContain(t, rec.Body.String(), `"n":1`, "1 von 5 Aufnahmen.", "Aufnahme 2 von 5")
	fi, err := os.Stat(filepath.Join(dir, "anna-20261027-190509.wav"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("not kept as anna-…wav 0600: %v %v", err, fi)
	}
	rec = stimmePost(t, cfg, take(16000, 1, 1.5, 0.3), "anna", nil) // the same second: a name beside it
	if _, err := os.Stat(filepath.Join(dir, "anna-20261027-190509-2.wav")); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("a second take in the same second: %d %v", rec.Code, err)
	}
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("a part stayed: %s", e.Name())
		}
	}
	_, body, _ = familyPage(t, Stimme, cfg, "GET", StimmePath, "family") // familyPage signs in as anna
	mustContain(t, body, "2 von 5 Aufnahmen.", "Aufnahme 3 von 5")
}

func TestStimmeRefusesWhatLaibCannotTake(t *testing.T) {
	joshua(t)
	dir := t.TempDir()
	cfg := StartConfig{WakeRecordings: dir}
	for name, c := range map[string]struct {
		b    []byte
		says string
	}{
		"not a WAV": {[]byte("hallo"), "keine Aufnahme, die die Box lesen kann"},
		"44.1 kHz":  {take(44100, 1, 1.5, 0.3), "keine Aufnahme, die die Box lesen kann"},
		"stereo":    {take(16000, 2, 1.5, 0.3), "keine Aufnahme, die die Box lesen kann"},
		"too short": {take(16000, 1, 0.1, 0.3), "zu kurz oder zu lang"},
		"too long":  {take(16000, 1, 5, 0.3), "zu kurz oder zu lang"},
		"silent":    {take(16000, 1, 1.5, 0), "nichts zu hören"},
		"clipped":   {take(16000, 1, 1.5, 1.5), "zu laut"},
	} {
		rec := stimmePost(t, cfg, c.b, "anna", nil)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.says) {
			t.Errorf("%s → %d %q", name, rec.Code, rec.Body)
		}
	}
	if rec := stimmePost(t, cfg, take(16000, 1, 1.5, 0.3), "anna", map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site → %d", rec.Code)
	}
	if rec := stimmePost(t, cfg, take(16000, 1, 1.5, 0.3), "", nil); rec.Code != http.StatusForbidden {
		t.Errorf("nobody → %d", rec.Code)
	}
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Errorf("something was kept: %v", es)
	}
	cfg.WakeRecordings = filepath.Join(dir, "fehlt")
	if rec := stimmePost(t, cfg, take(16000, 1, 1.5, 0.3), "anna", nil); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "nimmt gerade keine Aufnahmen") {
		t.Errorf("no folder → %d %q", rec.Code, rec.Body)
	}
}

// no folder from laib, or a persona without a wake word: no page, no entry
func TestStimmeOnlyWhereLaibTakesIt(t *testing.T) {
	joshua(t)
	if code, _, _ := familyPage(t, Stimme, StartConfig{}, "GET", StimmePath, "family"); code != http.StatusNotFound {
		t.Errorf("no folder → %d", code)
	}
	_, body, _ := familyPage(t, Mehr, StartConfig{}, "GET", mehrPath, "family")
	mustNotContain(t, body, "Weckwort")
	House.Wakeword = ""
	if code, _, _ := familyPage(t, Stimme, StartConfig{WakeRecordings: t.TempDir()}, "GET", StimmePath, "family"); code != http.StatusNotFound {
		t.Errorf("no wake word → %d", code)
	}
}
