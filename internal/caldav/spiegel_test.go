package caldav

import (
	"errors"
	"strings"
	"testing"
)

// The phone's text is cut into its cards as they came: folded lines,
// LF or CRLF, a nested AGENT card inside its own, Homes' own note dropped.
func TestSplitCards(t *testing.T) {
	text := "BEGIN:VCARD\nVERSION:3.0\nUID:a\nFN:Oma\n  Erna\nAGENT:\nBEGIN:VCARD\nUID:inner\nFN:Sekretär\nEND:VCARD\nX-HAUS-INHALT:alt\nEND:VCARD\n" +
		"noise\r\nbegin:vcard\r\nUID;VALUE=text: b \r\nFN:Ben\r\nEND:VCARD\r\n"
	cards, err := SplitCards(text)
	if err != nil || len(cards) != 2 {
		t.Fatalf("%+v %v", cards, err)
	}
	if cards[0].UID != "a" || cards[0].Name != "Oma Erna" || cards[1].UID != "b" || cards[1].Name != "Ben" {
		t.Fatalf("%+v", cards)
	}
	if strings.Contains(cards[0].Text, "X-HAUS-INHALT") || !strings.Contains(cards[0].Text, "UID:inner\r\n") || !strings.HasSuffix(cards[0].Text, "END:VCARD\r\nEND:VCARD\r\n") {
		t.Fatalf("%q", cards[0].Text)
	}
	if st := cards[0].stored(); storedInhalt(st) != cards[0].inhalt() || !strings.HasSuffix(st, "X-HAUS-INHALT:"+cards[0].inhalt()+"\r\nEND:VCARD\r\n") {
		t.Fatalf("stored %q", st)
	}
	var nouid *ErrNoUID
	if _, err := SplitCards("BEGIN:VCARD\nUID:a\nEND:VCARD\nBEGIN:VCARD\nFN:Ohne\nEND:VCARD\n"); !errors.As(err, &nouid) || nouid.N != 2 || nouid.Name != "Ohne" {
		t.Fatalf("no UID: %v", err)
	}
	for _, bad := range []string{"BEGIN:VCARD\nUID:a\n", "END:VCARD\n"} {
		if _, err := SplitCards(bad); !errors.Is(err, ErrBrokenCards) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if CardHref("a.b%2F/..") != CardHref("a.b%2F/..") || strings.Trim(strings.TrimSuffix(CardHref("x/../y"), ".vcf"), "0123456789abcdef") != "" {
		t.Fatalf("href %s", CardHref("x/../y"))
	}
}
