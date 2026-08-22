//go:build linux

package identity

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func apply(path string, identity ids) error {
	if os.Geteuid() != 0 {
		if os.Geteuid() == identity.uid && os.Getegid() == identity.gid {
			return nil
		}
		return fmt.Errorf("cannot apply PUID=%d PGID=%d without root privileges", identity.uid, identity.gid)
	}
	if err := filepath.WalkDir(path, func(entryPath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return os.Lchown(entryPath, identity.uid, identity.gid)
		}
		return os.Chown(entryPath, identity.uid, identity.gid)
	}); err != nil {
		return fmt.Errorf("change data ownership: %w", err)
	}
	if identity.uid == 0 && identity.gid == 0 {
		return nil
	}
	if err := unix.Setgroups([]int{identity.gid}); err != nil {
		return fmt.Errorf("set supplemental groups: %w", err)
	}
	if err := unix.Setgid(identity.gid); err != nil {
		return fmt.Errorf("set PGID: %w", err)
	}
	if err := unix.Setuid(identity.uid); err != nil {
		return fmt.Errorf("set PUID: %w", err)
	}
	return nil
}
