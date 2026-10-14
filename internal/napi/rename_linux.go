//go:build linux && (amd64 || arm64)

package napi

import (
	"os"
	"syscall"
	"unsafe"
)

const renameNoReplaceFlag = 1 // RENAME_NOREPLACE

// renameNoReplace renames oldname to newname inside dir, failing with
// EEXIST (fs.ErrExist) when newname exists: never a replace.
func renameNoReplace(dir *os.File, oldname, newname string) error {
	o, err := syscall.BytePtrFromString(oldname)
	if err != nil {
		return err
	}
	n, err := syscall.BytePtrFromString(newname)
	if err != nil {
		return err
	}
	fd := dir.Fd()
	_, _, e := syscall.Syscall6(sysRenameat2, fd, uintptr(unsafe.Pointer(o)), fd, uintptr(unsafe.Pointer(n)), renameNoReplaceFlag, 0)
	switch e {
	case 0:
		return nil
	case syscall.ENOSYS, syscall.EINVAL:
		return errNoRenameat2
	}
	return &os.LinkError{Op: "renameat2", Old: oldname, New: newname, Err: e}
}

// statfsFree is what an unprivileged writer may still put on dir's file system.
func statfsFree(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
