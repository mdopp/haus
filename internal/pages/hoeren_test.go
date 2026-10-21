package pages

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mdopp/haus/internal/persona"
)

// streamSay posts the field as the page's script does: in the background,
// asking for the answer as server-sent events. It returns the events'
// data, in order.
func streamSay(t *testing.T, h http.HandlerFunc, user, text string) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()
	rec := post(t, h, "/sprechen", url.Values{"nachricht": {text}}, user, map[string]string{"Accept": "text/event-stream"})
	var evs []map[string]any
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			var ev map[string]any
			if err := json.Unmarshal([]byte(d), &ev); err != nil {
				t.Fatalf("event %q: %v", d, err)
			}
			evs = append(evs, ev)
		}
	}
	return rec, evs
}

// fertig is the stream's last event: the exchange as the page shows it.
func fertig(t *testing.T, evs []map[string]any) string {
	t.Helper()
	if len(evs) == 0 {
		t.Fatal("no events")
	}
	s, ok := evs[len(evs)-1]["fertig"].(string)
	if !ok {
		t.Fatalf("the last event is %v, want fertig", evs[len(evs)-1])
	}
	return s
}

// haus#63: the page's script gets the answer in pieces: at once that the
// sentence arrived (the page shows "… denkt nach" from the first moment),
// then each piece of the reply, and at the end the exchange as a reload
// shows it — which /sprechen then does, too.
func TestSprechenStreamsTheReply(t *testing.T) {
	House = persona.Persona{Display: "Joshua"}
	defer func() { House = persona.Default() }()
	fake, bc := fakeBote(t)
	fake.Reply = "Der Wecker steht auf sieben, wie jeden Tag."
	h := Sprechen(StartConfig{Bote: bc})
	rec, evs := streamSay(t, h, "anna", "Weck mich um sieben")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "text/event-stream" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("→ %d %v", rec.Code, rec.Header())
	}
	if evs[0]["angekommen"] != true {
		t.Fatalf("the first event is %v", evs[0])
	}
	var pieces strings.Builder
	n := 0
	for _, ev := range evs[1 : len(evs)-1] {
		s, _ := ev["stueck"].(string)
		pieces.WriteString(s)
		n++
	}
	if n < 2 || pieces.String() != fake.Reply {
		t.Fatalf("%d pieces %q", n, pieces.String())
	}
	done := fertig(t, evs)
	mustContain(t, done, "<section class=gespraech", "Weck mich um sieben", "<span class=wer>Joshua</span>", fake.Reply)
	mustNotContain(t, done, "antwortet gerade nicht")
	_, body, _ := talkPage(t, h, "anna")
	mustContain(t, body, done)
	if len(fake.Seen()) != 1 {
		t.Fatal("the Bote was asked twice")
	}
}

// haus#63: the answer is never silence. A Bote that fails, or whose first
// word does not come within boteWait, ends the stream with the plain line
// — the persona does not answer, or has not yet — never its error.
func TestSprechenStreamSaysTimeoutAndFailurePlainly(t *testing.T) {
	House = persona.Persona{Display: "Joshua"}
	defer func() { House = persona.Default() }()
	wait := boteWait
	boteWait = 200 * time.Millisecond
	defer func() { boteWait = wait }()
	fake, bc := fakeBote(t)
	h := Sprechen(StartConfig{Bote: bc})

	fake.Hang = 5 * time.Second
	start := time.Now()
	_, evs := streamSay(t, h, "anna", "Wie wird das Wetter?")
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("the stream waited %s", d)
	}
	done := fertig(t, evs)
	mustContain(t, done, "Wie wird das Wetter?", "<p class=keine>Joshua hat nach zweieinhalb Minuten noch nicht geantwortet.")
	mustNotContain(t, done, ">Joshua<")
	mustNotLeak(t, done, "deadline", "context", "timeout", "Bote", "Hermes")

	failing, fc := fakeBote(t)
	failing.Status = 502
	_, evs = streamSay(t, Sprechen(StartConfig{Bote: fc}), "anna", "Hallo")
	done = fertig(t, evs)
	mustContain(t, done, "<p class=keine>Joshua antwortet gerade nicht. Versuch es gleich noch einmal.</p>")
	mustNotLeak(t, done, "502", "Agent run", "agent_incomplete", "Bote", "Hermes", "8642")
}

// boteWait is for the first word (haus#63: the first message after a model
// reload takes 40–50 s, with a tool more): two and a half minutes. A reply
// that has begun may take longer than that in all; it is not cut off.
func TestSprechenWaitsForTheFirstWordNotTheWhole(t *testing.T) {
	if boteWait < 150*time.Second || boteTurn < boteWait {
		t.Fatalf("boteWait %s, boteTurn %s", boteWait, boteTurn)
	}
	House = persona.Persona{Display: "Joshua"}
	defer func() { House = persona.Default() }()
	wait := boteWait
	boteWait = 150 * time.Millisecond
	defer func() { boteWait = wait }()
	fake, bc := fakeBote(t)
	fake.Reply, fake.Gap = "eins zwei drei vier fünf sechs", 60*time.Millisecond // ~300 ms in all
	h := Sprechen(StartConfig{Bote: bc})
	_, evs := streamSay(t, h, "anna", "Zähl bis sechs")
	done := fertig(t, evs)
	mustContain(t, done, "eins zwei drei vier fünf sechs")
	mustNotContain(t, done, "class=keine")
	// the plain form gets the same: the whole reply, after a 303
	if rec := say(t, h, "anna", "Noch einmal", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("→ %d", rec.Code)
	}
	_, body, _ := talkPage(t, h, "anna")
	mustContain(t, body, "Noch einmal", "eins zwei drei vier fünf sechs")
	mustNotContain(t, body, "class=keine")
}

// haus#63: /sprechen shows "… denkt nach" from the first moment: the field
// carries the persona's words for the script, which shows them with a
// running indicator the moment the sentence goes. Without script the form
// is the plain post it was.
func TestSprechenSaysItIsThinking(t *testing.T) {
	House = persona.Persona{Display: "Joshua"}
	defer func() { House = persona.Default() }()
	_, bc := fakeBote(t)
	_, body, _ := talkPage(t, Sprechen(StartConfig{Bote: bc}), "anna")
	mustContain(t, body, `<form method=post action=/sprechen class=sprechen data-hier data-wer="Joshua" data-denkt="Joshua denkt nach …">`,
		"name=nachricht", "<button class=senden>Senden</button>", "<script>", "'Accept':'text/event-stream'", "el('span','laeuft')", ".laeuft{",
		jsString(saysDown("")))
	mustNotContain(t, body, "class=mikro") // no speech-to-text set: no microphone
}

// speech is a WAV as the page's script makes it: 16 kHz, 16 bit, mono.
func speech(samples int) []byte {
	b := make([]byte, 44+samples*2)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+samples*2))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(samples*2))
	return b
}

// hear posts a recording as the page's script does.
func hear(t *testing.T, h http.HandlerFunc, body []byte, user string, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest("POST", HoerenPath, strings.NewReader(string(body)))
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
	h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// haus#63: the microphone's recording goes to Home Assistant's
// speech-to-text (Whisper on the box), through Haus, as it is — with the
// format HA needs — and the words come back for the field to send like
// typed ones. The microphone shows only when a provider is set.
func TestHoerenAsksHomeAssistantsSpeechToText(t *testing.T) {
	fake, hc := house(t)
	fake.SetSTT("stt.faster_whisper", "Mach das Licht im Flur an")
	cfg := StartConfig{HA: hc, STT: STT{Provider: "stt.faster_whisper", Language: "de"}}
	wav := speech(16000)
	code, out := hear(t, Hoeren(cfg), wav, "anna", nil)
	if code != 200 || out["ok"] != true || out["text"] != "Mach das Licht im Flur an" {
		t.Fatalf("→ %d %v", code, out)
	}
	heard := fake.Heard()
	if len(heard) != 1 || heard[0].Provider != "stt.faster_whisper" || string(heard[0].Body) != string(wav) ||
		heard[0].Content != "format=wav; codec=pcm; sample_rate=16000; bit_rate=16; channel=1; language=de" {
		t.Fatalf("HA heard %+v", heard)
	}
	// the microphone, beside the field, on Start and on /sprechen
	_, bc := fakeBote(t)
	cfg.Bote = bc
	_, start := startPage(t, cfg, "family")
	_, body, _ := talkPage(t, Sprechen(cfg), "anna")
	for _, page := range []string{start, body} {
		mustContain(t, page, `<button type=button class=mikro aria-label="Sprechen statt tippen" aria-pressed=false hidden>`, "'"+HoerenPath+"'", "getUserMedia")
		before(t, page, "name=nachricht", "class=mikro")
		before(t, page, "class=mikro", "class=senden")
	}
	// no provider, or no HA: no microphone, and nothing to post to
	for _, c := range []StartConfig{{HA: hc, Bote: bc}, {Bote: bc, STT: cfg.STT}, {HA: hc, Bote: bc, STT: STT{Provider: "../api/states"}}} {
		_, body, _ := talkPage(t, Sprechen(c), "anna")
		mustNotContain(t, body, "class=mikro")
		if code, _ := hear(t, Hoeren(c), wav, "anna", nil); code != http.StatusNotFound {
			t.Errorf("%+v: → %d, want 404", c.STT, code)
		}
	}
	if len(fake.Heard()) != 1 {
		t.Fatal("HA heard more")
	}
}

// What is no words is one plain line, never HA's detail: nothing heard,
// HA in trouble, a recording that is not the page's, too long. Only a
// person, only from Haus's own page, only POST.
func TestHoerenSaysPlainlyWhenThereAreNoWords(t *testing.T) {
	var logged strings.Builder
	log.SetOutput(&logged)
	defer log.SetOutput(os.Stderr)
	fake, hc := house(t)
	fake.SetSTT("stt.whisper", "")
	h := Hoeren(StartConfig{HA: hc, STT: STT{Provider: "stt.whisper"}})
	for what, c := range map[string]struct {
		body []byte
		code int
		says string
	}{
		"nothing heard": {speech(8000), 200, saysNotHeard},
		"not a wav":     {[]byte("OggS………………………………………………………………………………………………"), 400, saysBadSpeech},
		"stereo":        {func() []byte { b := speech(8000); binary.LittleEndian.PutUint16(b[22:], 2); return b }(), 400, saysBadSpeech},
		"too long":      {speech(maxSpeech / 2), 413, saysSpeechLong},
	} {
		code, out := hear(t, h, c.body, "anna", nil)
		if code != c.code || out["ok"] != false || out["says"] != c.says {
			t.Errorf("%s: → %d %v, want %d %q", what, code, out, c.code, c.says)
		}
	}
	fake.Down(true)
	code, out := hear(t, h, speech(8000), "anna", nil)
	if code != http.StatusBadGateway || out["says"] != saysNoSTT {
		t.Errorf("HA down: → %d %v", code, out)
	}
	if !strings.Contains(logged.String(), "speech-to-text") || strings.Contains(logged.String(), fake.Token) {
		t.Errorf("the log: %s", logged.String())
	}
	fake.Down(false)
	for _, c := range []struct {
		user string
		hdr  map[string]string
	}{{"", nil}, {"anna", map[string]string{"Sec-Fetch-Site": "cross-site"}}} {
		if code, _ := hear(t, h, speech(8000), c.user, c.hdr); code != http.StatusForbidden {
			t.Errorf("%q %v → %d, want 403", c.user, c.hdr, code)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", HoerenPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET → %d", rec.Code)
	}
}
