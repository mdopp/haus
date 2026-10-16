package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/dokumente"
	"github.com/mdopp/haus/internal/paperless"
)

// command runs one of Haus's one-shot commands instead of the server;
// getenv is the environment (os.Getenv), the container's own when Laib
// runs it with podman exec.
//   - export-documents: Paperless's documents into the folder qmd indexes
//     as the library's documents (haus#13); the refresh timer's first step,
//     before `qmd update`.
func command(ctx context.Context, args []string, getenv func(string) string, out, errw io.Writer) int {
	switch args[0] {
	case "export-documents":
		return exportDocuments(ctx, getenv, out, errw)
	}
	fmt.Fprintf(errw, "haus: unknown command %q; known: export-documents\n", args[0])
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
