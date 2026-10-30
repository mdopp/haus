package pages

import (
	"regexp"
	"strings"
	"testing"
)

// haus#194: the usage card is a white section on the dark Box page; a list
// in it (.zeilen) painted itself with the page's --card, dark, under the
// card's dark ink. A white section defines every colour token its content
// paints with — Box's .zeilen rules and its own — so a list reads in it in
// either theme, and its --card is white.
func TestWhiteSectionDefinesWhatItsListsPaintWith(t *testing.T) {
	rules := regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	defs := regexp.MustCompile(`(--[a-z-]+):([^;]+)`)
	uses := regexp.MustCompile(`var\((--[a-z-]+)\)`)
	defined := map[string]string{}
	var painted []string
	for _, m := range rules.FindAllStringSubmatch(usageStyle, -1) {
		sel := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(m[1]), "<style>"))
		if sel == ".genutzt" {
			for _, d := range defs.FindAllStringSubmatch(m[2], -1) {
				defined[d[1]] = strings.TrimSpace(d[2])
			}
		}
		for _, u := range uses.FindAllStringSubmatch(m[2], -1) {
			painted = append(painted, sel+" "+u[1])
		}
	}
	for _, m := range rules.FindAllStringSubmatch(adminStyle, -1) {
		if !strings.Contains(m[1], ".zeilen") {
			continue
		}
		for _, u := range uses.FindAllStringSubmatch(m[2], -1) {
			painted = append(painted, strings.TrimSpace(m[1])+" "+u[1])
		}
	}
	if len(painted) == 0 {
		t.Fatal("no rules read")
	}
	for _, p := range painted {
		tok := p[strings.LastIndex(p, " ")+1:]
		if _, ok := defined[tok]; !ok {
			t.Errorf("%s: %s is the page's, not the white section's", p, tok)
		}
	}
	if c := defined["--card"]; c != "#fff" && c != "#ffffff" {
		t.Errorf("--card in the white section is %q", c)
	}
}
