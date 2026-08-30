//go:build live

package telemetry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveEmbeddedSentryVerification(t *testing.T) {
	if os.Getenv("BEACON_LIVE_VERIFY") != "1" {
		t.Skip("set BEACON_LIVE_VERIFY=1 to send the controlled Beacon event")
	}
	installID, err := LoadOrCreateInstallID(filepath.Join(t.TempDir(), "install-id"))
	if err != nil {
		t.Fatal(err)
	}
	release := strings.TrimSpace(os.Getenv("GIT_TAG"))
	if release == "" {
		release = "telemetry-verification"
	}
	reporter := New(release, "verification", installID)
	if reporter.hub == nil {
		t.Fatal("Sentry reporter is disabled")
	}
	reporter.Capture(errors.New("controlled Beacon handled exception"), "warden.telemetry", "Controlled Beacon verification event")
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !reporter.Flush(flushCtx) {
		t.Fatal("controlled Beacon event did not flush before the timeout")
	}
}
