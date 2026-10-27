// Package code128 draws a Code 128 barcode in code set B (ISO/IEC 15417):
// the separator sheet's PATCH T (haus#161), which Laib's scan service and
// Paperless-ngx split a stack at. Only what that needs: printable ASCII,
// the checksum, an SVG.
package code128

import (
	"errors"
	"strconv"
	"strings"
)

const (
	startB = 104
	stop   = 106
)

// patterns is the symbol table: each value's bar and space widths in
// modules, bar first.
var patterns = [107]string{
	"212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213",
	"221312", "231212", "112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132",
	"221231", "213212", "223112", "312131", "311222", "321122", "321221", "312212", "322112", "322211",
	"212123", "212321", "232121", "111323", "131123", "131321", "112313", "132113", "132311", "211313",
	"231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121", "313121", "211331",
	"231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111",
	"314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214",
	"112412", "122114", "122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111",
	"111242", "121142", "121241", "114212", "124112", "124211", "411212", "421112", "421211", "212141",
	"214121", "412121", "111143", "111341", "131141", "114113", "114311", "411113", "411311", "113141",
	"114131", "311141", "411131", "211412", "211214", "211232", "2331112",
}

// B is s in code set B: the widths of its bars and spaces in modules,
// alternating and bar first, from Start B through the checksum to Stop.
func B(s string) ([]int, error) {
	if s == "" {
		return nil, errors.New("code128: nothing to encode")
	}
	values := []int{startB}
	sum := startB
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 32 || c > 126 {
			return nil, errors.New("code128: code set B has no " + strconv.QuoteRune(rune(c)))
		}
		values = append(values, int(c)-32)
		sum += (i + 1) * (int(c) - 32)
	}
	values = append(values, sum%103, stop)
	var w []int
	for _, v := range values {
		for _, d := range patterns[v] {
			w = append(w, int(d-'0'))
		}
	}
	return w, nil
}

// Modules is how many modules wide widths are.
func Modules(widths []int) int {
	n := 0
	for _, x := range widths {
		n += x
	}
	return n
}

// SVG draws widths with module mm per module, height mm tall, with quiet
// modules of white on either side; its size is in millimetres, for print,
// and its drawing in whole modules (viewBox), so no bar edge is rounded.
func SVG(widths []int, module, height float64, quiet int) string {
	mm := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	n := Modules(widths) + 2*quiet
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="` + mm(float64(n)*module) + `mm" height="` + mm(height) + `mm" viewBox="0 0 ` + strconv.Itoa(n) +
		` 1" preserveAspectRatio="none" shape-rendering="crispEdges" role="img" aria-label="Strichcode"><rect width="` + strconv.Itoa(n) + `" height="1" fill="#fff"/><g fill="#000">`)
	x := quiet
	for i, w := range widths {
		if i%2 == 0 { // a bar
			b.WriteString(`<rect x="` + strconv.Itoa(x) + `" width="` + strconv.Itoa(w) + `" height="1"/>`)
		}
		x += w
	}
	b.WriteString(`</g></svg>`)
	return b.String()
}
