package code128

import (
	"strings"
	"testing"
)

// The reference: the Code 128 symbol table (ISO/IEC 15417, table 1), the
// rows PATCH T needs, written down here apart from the package's table.
var reference = map[int]string{
	0:   "212222",  // space
	33:  "111323",  // A
	35:  "131321",  // C
	40:  "231113",  // H
	48:  "313121",  // P
	52:  "213311",  // T
	104: "211214",  // Start B
	106: "2331112", // Stop
}

// PATCH T in code set B: Start B, P A T C H space T, the checksum
// (104 + 1·48 + 2·33 + 3·52 + 4·35 + 5·40 + 6·0 + 7·52 = 1078; 1078 mod 103
// = 48, the value of P), Stop.
func TestPatchT(t *testing.T) {
	var want strings.Builder
	for _, v := range []int{104, 48, 33, 52, 35, 40, 0, 52, 48, 106} {
		want.WriteString(reference[v])
	}
	got, err := B("PATCH T")
	if err != nil {
		t.Fatal(err)
	}
	if s := widthString(got); s != want.String() {
		t.Fatalf("PATCH T:\n got %s\nwant %s", s, want.String())
	}
	if n := Modules(got); n != 11*9+13 {
		t.Errorf("modules = %d, want 112", n)
	}
}

// Every symbol of the table is three bars and three spaces of eleven
// modules (Stop: four bars, thirteen), and no two are alike.
func TestTable(t *testing.T) {
	seen := map[string]int{}
	for v, p := range patterns {
		sum := 0
		for _, c := range p {
			if c < '1' || c > '4' {
				t.Fatalf("value %d: %q", v, p)
			}
			sum += int(c - '0')
		}
		if want := map[bool]int{true: 13, false: 11}[v == stop]; sum != want || (v != stop && len(p) != 6) {
			t.Errorf("value %d: %q sums to %d", v, p, sum)
		}
		if o, ok := seen[p]; ok {
			t.Errorf("values %d and %d share %q", o, v, p)
		}
		seen[p] = v
	}
	if len(patterns) != 107 {
		t.Errorf("%d symbols, want 107", len(patterns))
	}
	for v, p := range reference {
		if patterns[v] != p {
			t.Errorf("value %d: %q, the reference says %q", v, patterns[v], p)
		}
	}
}

func TestRefuses(t *testing.T) {
	for _, s := range []string{"", "Ä", "a\tb", "\x7f"} {
		if _, err := B(s); err == nil {
			t.Errorf("B(%q) took it", s)
		}
	}
}

func TestSVG(t *testing.T) {
	w, _ := B("PATCH T")
	svg := SVG(w, 1.2, 30, 10)
	for _, want := range []string{`<svg xmlns="http://www.w3.org/2000/svg"`, `width="158.4mm"`, `height="30mm"`, `viewBox="0 0 132 1"`, `shape-rendering="crispEdges"`} {
		if !strings.Contains(svg, want) {
			t.Errorf("svg lacks %s:\n%s", want, svg)
		}
	}
	// one rect per bar: 3 per symbol, 4 in Stop
	if n := strings.Count(svg, "<rect "); n != 1+9*3+4 { // the white ground, then the bars
		t.Errorf("%d rects", n)
	}
	// the first bar starts after the quiet zone
	if !strings.Contains(svg, `<rect x="10" width="2" height="1"/>`) {
		t.Errorf("first bar not at the quiet zone's end:\n%s", svg)
	}
}

func widthString(w []int) string {
	var b strings.Builder
	for _, n := range w {
		b.WriteByte(byte('0' + n))
	}
	return b.String()
}
