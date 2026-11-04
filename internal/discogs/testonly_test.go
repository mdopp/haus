package discogs

import "context"

// Only the tests call these: production code calls what they wrap. They
// were in the package itself, where `deadcode` counted them as
// production code nobody runs (scripts/ratchet.py, test-only).

// Platten is the shelf as last read.
func (r *Regal) Platten(ctx context.Context) ([]Platte, error) {
	if _, _, err := r.Hat(ctx, "", "\x00"); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Platte(nil), r.plates...), nil
}
