package library

import "github.com/mdopp/homes/internal/qmd"

// Only the tests call these: production code calls what they wrap. They
// were in the package itself, where `deadcode` counted them as
// production code nobody runs (scripts/ratchet.py, test-only).

// Cut keeps the hits close enough to the question, in qmd's order. The
// Vermittler rounds a cosine to three places, so a hit can sit exactly on
// the bar; it stays, whatever the float arithmetic makes of 0.75 × 0.4.
func Cut(hits []qmd.Hit) []qmd.Hit {
	vecs := make([]float64, len(hits))
	for i, h := range hits {
		vecs[i] = h.Vec
	}
	out := []qmd.Hit{}
	for _, i := range cut(vecs) {
		out = append(out, hits[i])
	}
	return out
}
