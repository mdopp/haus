package schalter

// Only the tests call these: production code calls what they wrap. They
// were in the package itself, where `deadcode` counted them as
// production code nobody runs (scripts/ratchet.py, test-only).

// wait is until the notes under way are done (tests).
func (w *Witness) wait() { w.notes.Wait() }
