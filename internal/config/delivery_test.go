package config

import (
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestDeliveryFilesStayAlignedWithGoRuntime(t *testing.T) {
	t.Parallel()
	compose := readRepositoryFile(t, "../../compose.yaml")
	var composeDocument yaml.Node
	if err := yaml.Unmarshal(compose, &composeDocument); err != nil {
		t.Fatalf("compose.yaml: %v", err)
	}
	composeText := strings.ReplaceAll(string(compose), "\r\n", "\n")
	for _, want := range []string{
		"env_file:\n      - .env",
		"data:/app/data",
		"./config.yaml:/app/data/config.yaml",
		"restart: unless-stopped",
	} {
		if !strings.Contains(composeText, want) {
			t.Errorf("compose.yaml missing %q", want)
		}
	}
	if strings.HasPrefix(composeText, "name:") {
		t.Error("compose.yaml must not set a top-level project name")
	}
	if strings.Contains(composeText, "cap_drop:") || strings.Contains(composeText, "cap_add:") {
		t.Error("compose.yaml must preserve Docker's default capability set for recursive ownership changes")
	}

	dockerfile := string(readRepositoryFile(t, "../../Dockerfile"))
	for _, want := range []string{
		"golang:1.27-bookworm",
		"FROM scratch",
		"CGO_ENABLED=0",
		"/app/bin/warrden",
		"clear-missing",
		"clear-upgrades",
		"ca-certificates.crt",
		"chmod 1777 /rootfs/tmp",
		"GIT_TAG=dev",
		"GIT_HASH=unknown",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Errorf("Dockerfile missing %q", want)
		}
	}
	if strings.Contains(strings.ToLower(dockerfile), "alpine") {
		t.Error("Dockerfile must not use Alpine")
	}
	for _, forbidden := range []string{"ARG TARGETOS=", "ARG TARGETARCH="} {
		if strings.Contains(dockerfile, forbidden) {
			t.Errorf("Dockerfile must use BuildKit's automatic target value, found %q", forbidden)
		}
	}
	mainSource := string(readRepositoryFile(t, "../../cmd/warrden/main.go"))
	if !strings.Contains(mainSource, `_ "time/tzdata"`) {
		t.Error("cmd/warrden must embed timezone data for the scratch image")
	}

	environment := string(readRepositoryFile(t, "../../.env.example"))
	for _, key := range []string{"PUID=", "PGID=", "TZ=", "DRY_RUN="} {
		if !strings.Contains(environment, key) {
			t.Errorf(".env.example missing %s", key)
		}
	}
	for _, key := range []string{"APP_VERSION=", "CONFIG_PATH=", "HTTP_RETRY_COUNT=", "HTTP_TIMEOUT_SECONDS=", "DATABASE_PATH="} {
		if strings.Contains(environment, key) {
			t.Errorf(".env.example must omit %s", key)
		}
	}
	lines := strings.Split(strings.TrimSpace(environment), "\n")
	if len(lines) == 0 || lines[0] != "# --[ RUNTIME ]----------------------------------------------------------------" {
		t.Error(".env.example must start with the RUNTIME capsule header")
	}
	for _, line := range lines {
		if strings.HasPrefix(line, "# --[") && len(line) != 79 {
			t.Errorf(".env.example capsule header length=%d, want 79: %q", len(line), line)
		}
	}

	pipeline := readRepositoryFile(t, "../../.gitlab-ci.yml")
	var pipelineDocument yaml.Node
	if err := yaml.Unmarshal(pipeline, &pipelineDocument); err != nil {
		t.Fatalf(".gitlab-ci.yml: %v", err)
	}
	pipelineText := string(pipeline)
	for _, want := range []string{
		"go mod tidy -diff",
		"golangci-lint@v2.13.1",
		"govulncheck@v1.7.0",
		"CGO_ENABLED=1 go test -race ./...",
		"docker compose config --quiet",
		"--platform linux/arm64",
		"docker cp config.example.yaml warrden-container-test-config-writer:/app/data/config.yaml",
		"/app/bin/clear-missing",
		"America/Los_Angeles",
		"ca-certificates.crt",
		"docker stop --time 10",
	} {
		if !strings.Contains(pipelineText, want) {
			t.Errorf(".gitlab-ci.yml missing %q", want)
		}
	}
	for _, forbidden := range []string{"CONFIG_PATH=", "DATABASE_PATH=", "warden.db", "warrden-container-test-config:/config"} {
		if strings.Contains(pipelineText, forbidden) {
			t.Errorf(".gitlab-ci.yml contains removed runtime setting %q", forbidden)
		}
	}
	if strings.Contains(pipelineText, "$CI_PROJECT_DIR") {
		t.Error("container smoke tests must not bind runner-local paths through the host Docker daemon")
	}
}

func readRepositoryFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
