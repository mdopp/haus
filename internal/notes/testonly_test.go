package notes

// Only the tests call these: production code calls what they wrap. They
// were in the package itself, where `deadcode` counted them as
// production code nobody runs (scripts/ratchet.py, test-only).

// Create writes "# <title>\n\n<text>" as <slug>.md, or <slug>-2.md, -3, …
// when the name is taken: an existing note is never overwritten. The file
// appears complete or not at all: written aside, then hard-linked into
// place. A share without hard links gets an error, not a weaker write.
func (d *Dir) Create(title, text string) (string, error) {
	return d.CreateWith(title, text, Kopf{})
}
