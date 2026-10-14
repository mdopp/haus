//go:build !(linux && (amd64 || arm64))

package napi

import (
	"math"
	"os"
)

func renameNoReplace(*os.File, string, string) error { return errNoRenameat2 }

// statfsFree does not know here; the floor does not apply.
func statfsFree(string) (uint64, error) { return math.MaxUint64, nil }
