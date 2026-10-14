package napi

import "errors"

// errNoRenameat2 says renameat2(RENAME_NOREPLACE) is not there (another
// OS or architecture, an old kernel, a file system without it): place
// falls back to a hard link.
var errNoRenameat2 = errors.New("renameat2 RENAME_NOREPLACE not available")
