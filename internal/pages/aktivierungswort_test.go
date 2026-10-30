package pages

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mdopp/haus/internal/ha"
	"github.com/mdopp/haus/internal/ha/hafake"
)

// a Voice PE in the kitchen as HA lists it: its wake-word select (translation
// key wake_word, a config entity) beside its pipeline select, which is none
func voicePE(fake *hafake.HA, word string) {
	fake.SetRegistries([]map[string]any{{"area_id": "kueche", "name": "Küche"}}, nil, []map[string]any{
		{"entity_id": "select.voice_pe_aktivierungswort", "area_id": "kueche", "translation_key": "wake_word", "entity_category": "config"},
		{"entity_id": "select.voice_pe_assistent", "area_id": "kueche", "translation_key": "pipeline", "entity_category": "config"},
	})
	fake.Set("select.voice_pe_aktivierungswort", word, map[string]any{"friendly_name": "Voice PE Aktivierungswort", "options": []string{"Okay Nabu", "Hey Jarvis", "Hey Joshua"}})
	fake.Set("select.voice_pe_assistent", "Joshua", map[string]any{"friendly_name": "Voice PE Assistent"})
}

func talkWithHA(t *testing.T, cfg StartConfig) string {
	t.Helper()
	_, bc := fakeBote(t)
	cfg.Bote = bc
	code, body, _ := familyPage(t, Sprechen, cfg, "GET", "/sprechen", "family")
	if code != http.StatusOK {
		t.Fatalf("/sprechen → %d", code)
	}
	return body
}

// owner 2026-10-30: „Kannst du auf Haus darstellen, was das aktuelle Wakeword
// ist“ — the word the device reports comes first; the persona's phrase,
// set but not on the device yet, is said as such, with what works until then.
func TestWakeWordAsTheDeviceReportsIt(t *testing.T) {
	joshua(t)
	fake, hc := house(t)
	voicePE(fake, "Okay Nabu")
	cfg := StartConfig{HA: hc, WakeRecordings: t.TempDir()}

	code, body, _ := familyPage(t, Stimme, cfg, "GET", StimmePath, "family")
	if code != http.StatusOK {
		t.Fatalf("→ %d", code)
	}
	mustContain(t, body, "<h2 id=karte-aktivierungswort>Aktivierungswort</h2>", "So meldet es der Lautsprecher selbst.",
		"„Okay Nabu“", "Lautsprecher „Voice PE“ · Küche",
		"„Hey Joshua“ kommt noch auf den Lautsprecher", "„Hey Joshua“ ist eingestellt, aber noch nicht auf dem Lautsprecher. Bis dahin hört er auf „Okay Nabu“.",
		"Sag 5-mal „Hey Joshua“") // the recordings stay below
	before(t, body, "karte-aktivierungswort", "Einsprechen")
	if strings.Contains(body, "„Joshua“ ·") || strings.Contains(body, "Voice PE Assistent") {
		t.Errorf("the pipeline select is no wake word:\n%s", body)
	}
	mustContain(t, talkWithHA(t, cfg), `Am Lautsprecher sagst du zuerst „Okay Nabu“, dann deine Frage. <a href="/stimme">Aktivierungswort und Stimme</a>`)

	// once laib put the phrase on the device, that is all there is to say
	fake.Set("select.voice_pe_aktivierungswort", "Hey Joshua", map[string]any{"friendly_name": "Voice PE Aktivierungswort"})
	_, body, _ = familyPage(t, Stimme, cfg, "GET", StimmePath, "family")
	mustContain(t, body, "„Hey Joshua“ <span class=k>Lautsprecher „Voice PE“ · Küche</span>")
	if strings.Contains(body, "kommt noch") || strings.Contains(body, "Okay Nabu") {
		t.Errorf("the device has the phrase, nothing comes:\n%s", body)
	}
	mustContain(t, talkWithHA(t, cfg), "Am Lautsprecher sagst du zuerst „Hey Joshua“, dann deine Frage.")
}

// A device HA has no word from, two devices that differ, none at all: each
// is said, and the persona's phrase stands in as what is set.
func TestWakeWordWhenTheDevicesDoNotTell(t *testing.T) {
	joshua(t)
	fake, hc := house(t)
	voicePE(fake, "unavailable")
	cfg := StartConfig{HA: hc}

	code, body, _ := familyPage(t, Stimme, cfg, "GET", StimmePath, "family")
	if code != http.StatusOK {
		t.Fatalf("→ %d", code)
	}
	mustContain(t, body, "Lautsprecher „Voice PE“ · Küche <span class=k>antwortet gerade nicht; eingestellt ist „Hey Joshua“</span>")
	if strings.Contains(body, "Einsprechen") || strings.Contains(body, "kommt noch") {
		t.Errorf("no recordings here, and nothing heard instead:\n%s", body)
	}
	mustContain(t, talkWithHA(t, cfg), "Am Lautsprecher sagst du zuerst „Hey Joshua“, dann deine Frage.")

	// two that differ: each with its own; Sprechen points to the page
	fake.SetRegistries(nil, nil, []map[string]any{
		{"entity_id": "select.voice_pe_aktivierungswort", "translation_key": "wake_word"},
		{"entity_id": "select.bad_aktivierungswort", "translation_key": "wake_word"},
	})
	fake.Set("select.voice_pe_aktivierungswort", "Okay Nabu", map[string]any{"friendly_name": "Voice PE Aktivierungswort"})
	fake.Set("select.bad_aktivierungswort", "Hey Jarvis", map[string]any{"friendly_name": "Bad Wake word"})
	cfg.HA = freshClient(fake)
	_, body, _ = familyPage(t, Stimme, cfg, "GET", StimmePath, "family")
	mustContain(t, body, "„Hey Jarvis“ <span class=k>Lautsprecher „Bad“</span>", "„Okay Nabu“ <span class=k>Lautsprecher „Voice PE“</span>",
		"„Hey Joshua“ ist eingestellt, aber noch nicht auf dem Lautsprecher. Bis dahin hört er auf das Wort oben.")
	before(t, body, "Hey Jarvis", "Okay Nabu")
	mustContain(t, talkWithHA(t, cfg), "Am Lautsprecher sagst du zuerst sein Aktivierungswort, dann deine Frage. <a href=\"/stimme\">")

	// no device with a wake word in HA
	fake.SetRegistries(nil, nil, []map[string]any{{"entity_id": "light.k"}})
	cfg.HA = freshClient(fake)
	_, body, _ = familyPage(t, Stimme, cfg, "GET", StimmePath, "family")
	mustContain(t, body, "„Hey Joshua“ <span class=k>So ist es eingestellt. Kein Lautsprecher in Home Assistant meldet ein Aktivierungswort.</span>")
}

// HA not answering costs the device's word, not the page: the phrase as set,
// and that Haus cannot see the rest. Without HA the phrase is all there is.
func TestWakeWordWithoutHomeAssistant(t *testing.T) {
	joshua(t)
	fake, hc := house(t)
	voicePE(fake, "Okay Nabu")
	fake.Down(true)
	cfg := StartConfig{HA: hc}
	code, body, _ := familyPage(t, Stimme, cfg, "GET", StimmePath, "family")
	if code != http.StatusOK {
		t.Fatalf("HA down → %d", code)
	}
	mustContain(t, body, "„Hey Joshua“ <span class=k>So ist es eingestellt. Ob der Lautsprecher es schon kennt, kann Haus gerade nicht sehen.</span>")
	if strings.Contains(body, "Okay Nabu") {
		t.Errorf("nothing heard from HA:\n%s", body)
	}
	mustContain(t, talkWithHA(t, cfg), "Am Lautsprecher sagst du zuerst „Hey Joshua“, dann deine Frage.")

	cfg = StartConfig{WakeRecordings: t.TempDir()}
	_, body, _ = familyPage(t, Stimme, cfg, "GET", StimmePath, "family")
	mustContain(t, body, "„Hey Joshua“ <span class=k>So ist es eingestellt.</span>", "Einsprechen")

	// neither HA nor recordings: no page, and Sprechen says nothing of a speaker it cannot know
	if code, _, _ := familyPage(t, Stimme, StartConfig{}, "GET", StimmePath, "family"); code != http.StatusNotFound {
		t.Errorf("no HA, no recordings → %d", code)
	}
	if rec := stimmePost(t, StartConfig{HA: hc}, take(16000, 1, 1.5, 0.3), "anna", nil); rec.Code != http.StatusNotFound {
		t.Errorf("a take where none is kept → %d", rec.Code)
	}
	old := House
	House.Phrase = ""
	t.Cleanup(func() { House = old })
	if body := talkWithHA(t, StartConfig{}); strings.Contains(body, "Am Lautsprecher") {
		t.Errorf("nothing known, yet a line:\n%s", body)
	}
}

// freshClient is a client with an empty registry cache, so a changed registry is read.
func freshClient(f *hafake.HA) *ha.Client { return &ha.Client{URL: f.URL, Token: "ha-token"} }
