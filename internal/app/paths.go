package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	dataDirectory      = "data"
	configPath         = filepath.Join(dataDirectory, "config.yaml")
	databasePath       = filepath.Join(dataDirectory, "warrden.db")
	legacyDatabasePath = filepath.Join(dataDirectory, "warden.db")
	installIDPath      = filepath.Join(dataDirectory, "install-id")
)

type databaseFile struct {
	oldPath string
	newPath string
}

func migrateLegacyDatabase(oldPath, newPath string) (bool, error) {
	return migrateLegacyDatabaseWith(oldPath, newPath, os.Rename)
}

func migrateLegacyDatabaseWith(oldPath, newPath string, rename func(string, string) error) (bool, error) {
	oldMain, err := pathExists(oldPath)
	if err != nil {
		return false, fmt.Errorf("inspect legacy database: %w", err)
	}
	newMain, err := pathExists(newPath)
	if err != nil {
		return false, fmt.Errorf("inspect current database: %w", err)
	}
	oldWAL, err := pathExists(oldPath + "-wal")
	if err != nil {
		return false, fmt.Errorf("inspect legacy database WAL: %w", err)
	}
	oldSHM, err := pathExists(oldPath + "-shm")
	if err != nil {
		return false, fmt.Errorf("inspect legacy database SHM: %w", err)
	}
	newWAL, err := pathExists(newPath + "-wal")
	if err != nil {
		return false, fmt.Errorf("inspect current database WAL: %w", err)
	}
	newSHM, err := pathExists(newPath + "-shm")
	if err != nil {
		return false, fmt.Errorf("inspect current database SHM: %w", err)
	}

	if newMain {
		if oldMain || oldWAL || oldSHM {
			return false, fmt.Errorf("both legacy and current database files exist; refusing automatic migration")
		}
		return false, nil
	}
	if newWAL || newSHM {
		return false, fmt.Errorf("current database sidecar exists without %s", newPath)
	}
	if !oldMain {
		if oldWAL || oldSHM {
			return false, fmt.Errorf("legacy database sidecar exists without %s", oldPath)
		}
		return false, nil
	}

	files := []databaseFile{{oldPath: oldPath, newPath: newPath}}
	if oldWAL {
		files = append(files, databaseFile{oldPath: oldPath + "-wal", newPath: newPath + "-wal"})
	}
	if oldSHM {
		files = append(files, databaseFile{oldPath: oldPath + "-shm", newPath: newPath + "-shm"})
	}

	moved := make([]databaseFile, 0, len(files))
	for _, file := range files {
		if err := rename(file.oldPath, file.newPath); err != nil {
			migrationErr := fmt.Errorf("rename %s to %s: %w", file.oldPath, file.newPath, err)
			var rollbackErr error
			for index := len(moved) - 1; index >= 0; index-- {
				file := moved[index]
				if err := rename(file.newPath, file.oldPath); err != nil {
					rollbackErr = errors.Join(rollbackErr, fmt.Errorf("restore %s to %s: %w", file.newPath, file.oldPath, err))
				}
			}
			return false, errors.Join(migrationErr, rollbackErr)
		}
		moved = append(moved, file)
	}
	return true, nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
