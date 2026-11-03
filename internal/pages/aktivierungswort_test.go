package pages

import (
	"net/http"
	"strings"
	"testing"

	"github.com/mdopp/homes/internal/ha"
	"github.com/mdopp/homes/internal/ha/hafake"
)

// a Voice PE in the kitchen as HA lists it: its wake-word select (translation
// key wake_word, a config entity) beside its pipeline select, which is none
func voicePE(fake *hafake.HA, word string) {
	fake.SetRegistries([]map[string]any{{"area_id": "kueche", "name": "Küche"}}, nil, []map[string]any{
		{"entity_id": "select.voice_pe_aktivierungswort", "area_id": "kueche", "translation_key": "wake_word", "entity_category": "config"},
		{"entity_id": "select.voice_pe_assistent", "area_id": "kueche", "translation_key": "pipeline", "entity_category": "config"},
	})
	fake.Set("select.voice_pe_aktivierungswort", word, map[string]any{"friendly_name": "Voice PE Aktivierungswort", "options": []string{"Okay Nabu", "Hey Jarvis", "Hey Mycroft"}})
	fake.Set("select.voice_pe_assistent", "Homes", map[string]any{"friendly_name": "Voice PE Assistent"})
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

// owner 2026-10-30: „Kannst du auf Homes darstellen, was das aktuelle Wakeword
// ist“ — the word the device reports comes first; „Hey Mycroft“, which the
// device offers and Laib selects, is said as still to be chosen, with what
// works until then.
func TestWakeWordAsTheDeviceReportsIt(t *testing.T) {
	fake, hc := house(t)
	voicePE(fake, "Okay Nabu")
	cfg := StartConfig{HA: hc}

	code, body, _ := familyPage(t, Aktivierungswort, cfg, "GET", AktivierungswortPath, "family")
	if code != http.StatusOK {
		t.Fatalf("→ %d", code)
	}
	mustContain(t, body, "<title>Aktivierungswort · Homes</title>", "<h2 id=karte-aktivierungswort>Aktivierungswort</h2>", "So meldet es der Lautsprecher selbst.",
		"„Okay Nabu“", "Lautsprecher „Voice PE“ · Küche",
		"„Hey Mycroft“ wird noch ausgewählt", "„Hey Mycroft“ kann der Lautsprecher schon, Laib wählt es für ihn aus. Bis dahin hört er auf „Okay Nabu“.")
	mustNotContain(t, body, "Einsprechen", "Aufnahme", "aufgespielt", "beibringen")
	if strings.Contains(body, "„Homes“ ·") || strings.Contains(body, "Voice PE Assistent") {
		t.Errorf("the pipeline select is no wake word:\n%s", body)
	}
	mustContain(t, talkWithHA(t, cfg), `Am Lautsprecher sagst du zuerst „Okay Nabu“, dann deine Frage. <a href="/aktivierungswort">Aktivierungswort</a>`)

	// once Laib selected it on the device, that is all there is to say
	fake.Set("select.voice_pe_aktivierungswort", "Hey Mycroft", map[string]any{"friendly_name": "Voice PE Aktivierungswort"})
	_, body, _ = familyPage(t, Aktivierungswort, cfg, "GET", AktivierungswortPath, "family")
	mustContain(t, body, "„Hey Mycroft“ <span class=k>Lautsprecher „Voice PE“ · Küche</span>")
	if strings.Contains(body, "ausgewählt") || strings.Contains(body, "Okay Nabu") {
		t.Errorf("the device has the phrase, nothing to choose:\n%s", body)
	}
	mustContain(t, talkWithHA(t, cfg), "Am Lautsprecher sagst du zuerst „Hey Mycroft“, dann deine Frage.")

	// a POST changes nothing: the page only shows
	if code, _, _ := familyPage(t, Aktivierungswort, cfg, "POST", AktivierungswortPath, "family"); code != http.StatusMethodNotAllowed {
		t.Errorf("POST → %d", code)
	}
}

// A device HA has no word from, two devices that differ, none at all: each
// is said, and „Hey Mycroft“ stands in as what is set.
func TestWakeWordWhenTheDevicesDoNotTell(t *testing.T) {
	fake, hc := house(t)
	voicePE(fake, "unavailable")
	cfg := StartConfig{HA: hc}

	code, body, _ := familyPage(t, Aktivierungswort, cfg, "GET", AktivierungswortPath, "family")
	if code != http.StatusOK {
		t.Fatalf("→ %d", code)
	}
	mustContain(t, body, "Lautsprecher „Voice PE“ · Küche <span class=k>antwortet gerade nicht; eingestellt ist „Hey Mycroft“</span>")
	if strings.Contains(body, "ausgewählt") {
		t.Errorf("nothing heard instead:\n%s", body)
	}
	mustContain(t, talkWithHA(t, cfg), "Am Lautsprecher sagst du zuerst „Hey Mycroft“, dann deine Frage.")

	// two that differ: each with its own; Sprechen points to the page
	fake.SetRegistries(nil, nil, []map[string]any{
		{"entity_id": "select.voice_pe_aktivierungswort", "translation_key": "wake_word"},
		{"entity_id": "select.bad_aktivierungswort", "translation_key": "wake_word"},
	})
	fake.Set("select.voice_pe_aktivierungswort", "Okay Nabu", map[string]any{"friendly_name": "Voice PE Aktivierungswort"})
	fake.Set("select.bad_aktivierungswort", "Hey Jarvis", map[string]any{"friendly_name": "Bad Wake word"})
	cfg.HA = freshClient(fake)
	_, body, _ = familyPage(t, Aktivierungswort, cfg, "GET", AktivierungswortPath, "family")
	mustContain(t, body, "„Hey Jarvis“ <span class=k>Lautsprecher „Bad“</span>", "„Okay Nabu“ <span class=k>Lautsprecher „Voice PE“</span>",
		"„Hey Mycroft“ kann der Lautsprecher schon, Laib wählt es für ihn aus. Bis dahin hört er auf das Wort oben.")
	before(t, body, "Hey Jarvis", "Okay Nabu")
	mustContain(t, talkWithHA(t, cfg), "Am Lautsprecher sagst du zuerst sein Aktivierungswort, dann deine Frage. <a href=\"/aktivierungswort\">")

	// no device with a wake word in HA
	fake.SetRegistries(nil, nil, []map[string]any{{"entity_id": "light.k"}})
	cfg.HA = freshClient(fake)
	_, body, _ = familyPage(t, Aktivierungswort, cfg, "GET", AktivierungswortPath, "family")
	mustContain(t, body, "„Hey Mycroft“ <span class=k>So ist es eingestellt. Kein Lautsprecher in Home Assistant meldet ein Aktivierungswort.</span>")
}

// HA not answering costs the device's word, not the page: „Hey Mycroft“ as
// set, and that Homes cannot see the rest. Without HA there is no page and
// no line: no speaker Homes could know of.
func TestWakeWordWithoutHomeAssistant(t *testing.T) {
	fake, hc := house(t)
	voicePE(fake, "Okay Nabu")
	fake.Down(true)
	cfg := StartConfig{HA: hc}
	code, body, _ := familyPage(t, Aktivierungswort, cfg, "GET", AktivierungswortPath, "family")
	if code != http.StatusOK {
		t.Fatalf("HA down → %d", code)
	}
	mustContain(t, body, "„Hey Mycroft“ <span class=k>So ist es eingestellt. Was der Lautsprecher gerade hört, kann Homes im Moment nicht sehen.</span>")
	if strings.Contains(body, "Okay Nabu") {
		t.Errorf("nothing heard from HA:\n%s", body)
	}
	mustContain(t, talkWithHA(t, cfg), "Am Lautsprecher sagst du zuerst „Hey Mycroft“, dann deine Frage.")

	if code, _, _ := familyPage(t, Aktivierungswort, StartConfig{}, "GET", AktivierungswortPath, "family"); code != http.StatusNotFound {
		t.Errorf("no HA → %d", code)
	}
	if body := talkWithHA(t, StartConfig{}); strings.Contains(body, "Am Lautsprecher") {
		t.Errorf("no HA, yet a line:\n%s", body)
	}
}

// freshClient is a client with an empty registry cache, so a changed registry is read.
func freshClient(f *hafake.HA) *ha.Client { return &ha.Client{URL: f.URL, Token: "ha-token"} }
