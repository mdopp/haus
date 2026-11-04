// Package jsonfile is how Homes keeps its own small files in HOMES_DATA
// (the favourites, the calendar view, the paired phones, the connected
// accounts, the wishes, the subscriptions, the bin): one JSON file each,
// replaced whole or not at all. Written beside the file under a hidden
// name, synced, renamed over it, then the folder synced: after a power cut
// a reader finds the old file or the new one, never half of one (as laib's
// boxfs.WriteAtomic, laib#148).
package jsonfile

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Save writes v to path as indented JSON, readable by Homes' user only.
// The hidden temp file (.<name>-<random>) does not end in .json, so a
// folder listing that looks for *.json never sees it.
func Save(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // gone after the rename; left over only on failure
	_, err = tmp.Write(b)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
