package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mdopp/homes/internal/caldav"
)

// The household's firm cards carried one „Quelle: <link>“ note line per
// document kontakt_ergaenzen read; the contact's „Dokumente“ link (#281)
// replaces them, and kontakt_ergaenzen writes none any more (owner
// 2026-11-05). quellenWeg takes those lines out once, at start.

// quelleDir is in HOMES_DATA: each changed card as it was (<UID>.vcf, the
// first copy kept), and the marker that it ran (fertig).
const quelleDir = "kontakte-quelle"

// quelleLine is a line kontakt_ergaenzen wrote: exactly „Quelle: “ and one
// link to a Paperless document. A line the family wrote another way stays.
var quelleLine = regexp.MustCompile(`^Quelle: https?://\S+/documents/[0-9]+/\S*$`)

// withoutQuelle is a firm card's note without those lines, and whether
// there were any; a card the family made is never changed.
func withoutQuelle(ct caldav.Contact) (string, bool) {
	if !ct.IsFirm() {
		return ct.Note, false
	}
	var keep []string
	for _, l := range strings.Split(ct.Note, "\n") {
		if !quelleLine.MatchString(l) {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n"), len(keep) < strings.Count(ct.Note, "\n")+1
}

// quellenWeg takes the lines out of the household's address book and
// marks it done; a card that cannot be written back (a file of several
// cards) is left and counted. A failure leaves the marker out, and the
// next run takes up what is left; changed counts the cards written.
func quellenWeg(ctx context.Context, c *caldav.Client, dataDir string) (changed, left int, done bool, err error) {
	dir := filepath.Join(dataDir, quelleDir)
	if _, err := os.Stat(filepath.Join(dir, "fertig")); err == nil {
		return 0, 0, true, nil
	}
	cards, partial, err := c.Cards(ctx)
	if err != nil {
		return 0, 0, false, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, 0, false, err
	}
	for _, cd := range cards {
		note, ok := withoutQuelle(cd.Contact)
		if !ok {
			continue
		}
		if cd.Text() == "" {
			left++
			continue
		}
		if err := keepCopy(dir, cd); err != nil {
			return changed, left, false, err
		}
		cd.Note = note
		if err := c.AmendCard(ctx, cd, nil, ""); err != nil {
			return changed, left, false, fmt.Errorf("%s: %w", cd.UID, err)
		}
		changed++
	}
	if partial {
		return changed, left, false, errors.New("the address book was read only in part")
	}
	return changed, left, false, os.WriteFile(filepath.Join(dir, "fertig"), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

// keepCopy writes the card as Radicale has it, once: a first copy is the
// one before any change and is never written over.
func keepCopy(dir string, cd caldav.Card) error {
	f, err := os.OpenFile(filepath.Join(dir, url.PathEscape(strings.TrimSpace(cd.UID))+".vcf"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := f.WriteString(cd.Text()); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// quellenWegStart runs quellenWeg until it is done, with a growing pause
// while Radicale does not answer, and logs what it changed; without the
// address book (module kontakte off) nothing.
func quellenWegStart(ctx context.Context, c *caldav.Client, dataDir string) {
	if c == nil {
		return
	}
	for wait := 10 * time.Second; ; wait = min(wait*2, 10*time.Minute) {
		tctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		changed, left, already, err := quellenWeg(tctx, c, dataDir)
		cancel()
		if changed > 0 || left > 0 || (err == nil && !already) {
			log.Printf("homes: Kontakte: „Quelle:“ lines taken out of %d cards (each as it was in %s), %d left (a file of several cards)", changed, filepath.Join(dataDir, quelleDir), left)
		}
		if err == nil {
			return
		}
		log.Printf("homes: Kontakte: taking out the „Quelle:“ lines: %v (again in %v)", err, wait)
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
