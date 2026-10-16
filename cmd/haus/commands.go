package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/caldav"
	"github.com/mdopp/haus/internal/dokumente"
	"github.com/mdopp/haus/internal/notes"
	"github.com/mdopp/haus/internal/paperless"
	"github.com/mdopp/haus/internal/takeout"
)

// command runs one of Haus's one-shot commands instead of the server;
// getenv is the environment (os.Getenv), the container's own when Laib
// runs it with podman exec.
//   - export-documents: Paperless's documents into the folder qmd indexes
//     as the library's documents (haus#13); the refresh timer's first step,
//     before `qmd update`.
//   - import-takeout: a Google Takeout archive into the Haushalt calendar,
//     the Haushalt address book and the notes folder, once (haus#13, laib
//     ADR 0017 §4).
func command(ctx context.Context, args []string, getenv func(string) string, out, errw io.Writer) int {
	switch args[0] {
	case "export-documents":
		return exportDocuments(ctx, getenv, out, errw)
	case "import-takeout":
		return importTakeout(ctx, args[1:], getenv, out, errw)
	}
	fmt.Fprintf(errw, "haus: unknown command %q; known: export-documents, import-takeout\n", args[0])
	return 2
}

// exportTime bounds one export: the first one reads every document.
var exportTime = 30 * time.Minute

// exportDocuments mirrors Paperless into HAUS_DOCUMENTS_DIR. Paperless in
// trouble (down, refusing the token) is not the refresh's failure: the
// folder stays as it was, the notes are refreshed all the same, and the
// pages say the documents do not answer. Only the folder failing is.
func exportDocuments(ctx context.Context, getenv func(string) string, out, errw io.Writer) int {
	dir := getenv("HAUS_DOCUMENTS_DIR")
	u := strings.TrimRight(getenv("HAUS_PAPERLESS_URL"), "/")
	tok := getenv("HAUS_PAPERLESS_TOKEN")
	if dir == "" || u == "" || tok == "" {
		fmt.Fprintln(out, "haus export-documents: HAUS_DOCUMENTS_DIR or Paperless not set; nothing to export")
		return 0
	}
	ctx, cancel := context.WithTimeout(ctx, exportTime)
	defer cancel()
	rep, err := dokumente.Mirror(ctx, &paperless.Client{URL: u, Token: tok}, dir)
	var src dokumente.SourceError
	switch {
	case errors.As(err, &src):
		fmt.Fprintf(errw, "haus export-documents: %v; the documents stay as they were\n", err)
		return 0
	case err != nil:
		fmt.Fprintf(errw, "haus export-documents: %v\n", err)
		return 1
	}
	fmt.Fprintf(out, "haus export-documents: %d written, %d unchanged, %d removed\n", rep.Written, rep.Unchanged, rep.Removed)
	return 0
}

// names is a flag that may be given more than once.
type names []string

func (n *names) String() string     { return strings.Join(*n, ", ") }
func (n *names) Set(v string) error { *n = append(*n, v); return nil }

// importTakeout is `haus import-takeout [-dry-run] [-skip-calendar Name]…
// <zip>…`: Haus's own Radicale account and notes folder (the Haushalt's,
// HAUS_RADICALE_*, HAUS_NOTES_DIR) are where it writes; one not set is
// left out, said so. It says in German what it did, for the person who
// runs it; 1 when something was not written.
func importTakeout(ctx context.Context, args []string, getenv func(string) string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("import-takeout", flag.ContinueOnError)
	fs.SetOutput(errw)
	dry := fs.Bool("dry-run", false, "nur zählen, nichts schreiben")
	var skip names
	fs.Var(&skip, "skip-calendar", "diesen Kalender (nach Namen) auslassen; mehrfach möglich")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(errw, "Aufruf: haus import-takeout [-dry-run] [-skip-calendar Name]… <takeout.zip>…")
		return 2
	}
	t := takeout.Target{SkipCalendars: skip, DryRun: *dry, Loc: time.Local}
	if tz := getenv("TZ"); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			t.Loc = l
		}
	}
	if u, user, pw := strings.TrimRight(getenv("HAUS_RADICALE_URL"), "/"), getenv("HAUS_RADICALE_USER"), getenv("HAUS_RADICALE_PASSWORD"); u != "" && user != "" && pw != "" {
		t.Radicale = &caldav.Client{URL: u, User: user, Password: pw, Calendar: getenv("HAUS_RADICALE_CALENDAR"),
			AddressBook: getenv("HAUS_RADICALE_ADDRESSBOOK"), Loc: t.Loc}
	} else {
		fmt.Fprintln(out, "Kein Radicale eingerichtet (HAUS_RADICALE_URL, _USER, _PASSWORD): Kalender und Kontakte werden ausgelassen.")
	}
	if d := getenv("HAUS_NOTES_DIR"); d != "" {
		n, err := notes.Open(d)
		if err != nil {
			fmt.Fprintf(errw, "Der Notizen-Ordner %s ist nicht lesbar: %v\n", d, err)
			return 1
		}
		t.Notes = n
	} else {
		fmt.Fprintln(out, "Kein Notizen-Ordner eingerichtet (HAUS_NOTES_DIR): Google Notizen werden ausgelassen.")
	}
	rep, err := takeout.Import(ctx, fs.Args(), t, errw)
	if *dry {
		fmt.Fprintln(out, "Probelauf: geschrieben wurde nichts. So viel würde übernommen:")
	}
	if len(rep.Calendars) > 0 {
		fmt.Fprintf(out, "Kalender (%s): %s\n", strings.Join(rep.Calendars, ", "), said(rep.Events, "Termine"))
	}
	if len(rep.Skipped) > 0 {
		fmt.Fprintf(out, "Ausgelassen: %s\n", strings.Join(rep.Skipped, ", "))
	}
	if t.Radicale != nil {
		fmt.Fprintf(out, "Kontakte: %s\n", said(rep.Contacts, "Kontakte"))
	}
	if t.Notes != nil {
		fmt.Fprintf(out, "Google Notizen (Ordner „%s“): %s; Anhänge: %s\n", takeout.Folder, said(rep.Notes, "Notizen"), said(rep.Attachments, "Dateien"))
	}
	if err != nil {
		fmt.Fprintf(errw, "Abgebrochen: %v\n", err)
		return 1
	}
	if rep.Events.Failed+rep.Contacts.Failed+rep.Notes.Failed+rep.Attachments.Failed > 0 {
		fmt.Fprintln(out, "Nicht alles wurde übernommen; was fehlt und warum, steht oben. Ein zweiter Lauf schreibt nichts doppelt.")
		return 1
	}
	return 0
}

// one is each plural's singular, for a count of 1.
var one = map[string]string{"Termine": "Termin", "Kontakte": "Kontakt", "Notizen": "Notiz", "Dateien": "Datei"}

// said is a count in words: "12 Termine neu, 3 schon da".
func said(c takeout.Count, what string) string {
	if c.New == 1 {
		what = one[what]
	}
	s := fmt.Sprintf("%d %s neu", c.New, what)
	if c.There > 0 {
		s += fmt.Sprintf(", %d schon da", c.There)
	}
	if c.Failed > 0 {
		s += fmt.Sprintf(", %d nicht übernommen", c.Failed)
	}
	return s
}
