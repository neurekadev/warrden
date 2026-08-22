// Package identity validates and applies application-managed PUID/PGID settings.
package identity

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ids struct{ uid, gid int }

// Prepare creates the writable data path and permanently applies PUID/PGID on Linux.
func Prepare(path string) error {
	configured, err := configuredIDs()
	if err != nil {
		return err
	}
	absPath, err := ownershipPath(path, configured)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	return apply(absPath, configured)
}

func ownershipPath(path string, configured ids) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve data directory: %w", err)
	}
	clean := filepath.Clean(absPath)
	root := filepath.VolumeName(clean) + string(filepath.Separator)
	if strings.EqualFold(clean, root) {
		return "", fmt.Errorf("refusing to recursively change ownership of filesystem root %q", clean)
	}
	return clean, nil
}

func configuredIDs() (ids, error) {
	puid, hasUID := os.LookupEnv("PUID")
	pgid, hasGID := os.LookupEnv("PGID")
	if hasUID != hasGID {
		return ids{}, fmt.Errorf("PUID and PGID must be set together")
	}
	if !hasUID {
		puid, pgid = "1000", "1000"
	}
	uidValue, err := strconv.ParseUint(puid, 10, 32)
	if err != nil || uidValue == 1<<32-1 {
		return ids{}, fmt.Errorf("PUID must be a non-negative integer")
	}
	gidValue, err := strconv.ParseUint(pgid, 10, 32)
	if err != nil || gidValue == 1<<32-1 {
		return ids{}, fmt.Errorf("PGID must be a non-negative integer")
	}
	uid, gid := int(uidValue), int(gidValue)
	if (uid == 0) != (gid == 0) {
		return ids{}, fmt.Errorf("PUID and PGID must both be non-zero or both be 0")
	}
	return ids{uid: uid, gid: gid}, nil
}
