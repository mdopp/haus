package namen

import "testing"

func TestPlain(t *testing.T) {
	if got := Plain("Ärzte & Co."); got != "arzteundco" {
		t.Errorf("%q", got)
	}
}
