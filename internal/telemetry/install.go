package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrCreateInstallID returns the stable UUID stored for this wArrden installation.
func LoadOrCreateInstallID(path string) (string, error) {
	installID, err := readInstallID(path)
	if err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			return "", fmt.Errorf("secure installation ID: %w", err)
		}
		return installID, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create installation ID directory: %w", err)
	}

	installID, err = randomUUID()
	if err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".install-id-*")
	if err != nil {
		return "", fmt.Errorf("create temporary installation ID: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("secure temporary installation ID: %w", err)
	}
	if _, err := temporary.WriteString(installID + "\n"); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write installation ID: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync installation ID: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close installation ID: %w", err)
	}
	if err := os.Link(temporaryPath, path); errors.Is(err, os.ErrExist) {
		return readInstallID(path)
	} else if err != nil {
		return "", fmt.Errorf("persist installation ID: %w", err)
	}
	return installID, nil
}

func readInstallID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	installID := strings.TrimSpace(string(data))
	if !validUUID(installID) {
		return "", fmt.Errorf("installation ID in %s is not a valid UUID", path)
	}
	return installID, nil
}

func randomUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate installation ID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := strings.ReplaceAll(value, "-", "")
	if len(compact) != 32 {
		return false
	}
	_, err := hex.DecodeString(compact)
	return err == nil
}
