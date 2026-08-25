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
		"image: ghcr.io/neurekadev/warrden:4",
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

	readme := string(readRepositoryFile(t, "../../README.md"))
	for _, want := range []string{
		"https://github.com/neurekadev/warrden",
		"https://raw.githubusercontent.com/neurekadev/warrden/refs/heads/main/compose.yaml",
		"> [!CAUTION]",
		"Images at `registry.neureka.dev/warrden/warrden` are no longer updated. Use `ghcr.io/neurekadev/warrden`.",
	} {
		if !strings.Contains(readme, want) {
			t.Errorf("README.md missing %q", want)
		}
	}
	for _, forbidden := range []string{"code.neureka.dev", "NeurekaSoftware/wArrden", "official GitLab repository"} {
		if strings.Contains(readme, forbidden) {
			t.Errorf("README.md contains retired repository reference %q", forbidden)
		}
	}

	changelog := string(readRepositoryFile(t, "../../CHANGELOG.md"))
	if strings.Contains(changelog, "https://code.neureka.dev") {
		t.Error("CHANGELOG.md contains retired repository links")
	}
	if !strings.Contains(changelog, "[Unreleased]: https://github.com/neurekadev/warrden/compare/") {
		t.Error("CHANGELOG.md must compare releases in the GitHub repository")
	}

	module := string(readRepositoryFile(t, "../../go.mod"))
	if !strings.HasPrefix(module, "module github.com/neurekadev/warrden\n") {
		t.Error("go.mod must use the GitHub repository module path")
	}

	workflow := readRepositoryFile(t, "../../.github/workflows/ci.yml")
	var workflowDocument yaml.Node
	if err := yaml.Unmarshal(workflow, &workflowDocument); err != nil {
		t.Fatalf(".github/workflows/ci.yml: %v", err)
	}
	workflowText := string(workflow)
	for _, want := range []string{
		"pull_request:",
		"workflow_dispatch:",
		"actions/checkout@v7",
		"actions/setup-go@v7",
		"go mod tidy -diff",
		"golangci/golangci-lint-action@v9",
		"version: v2.13.1",
		"govulncheck@v1.7.0",
		"CGO_ENABLED=1 go test -race ./...",
		"docker compose config --quiet",
		"--platform linux/arm64",
		"docker cp config.example.yaml warrden-container-test-config-writer:/app/data/config.yaml",
		"/app/bin/clear-missing",
		"America/Los_Angeles",
		"ca-certificates.crt",
		"docker stop --time 10",
		"ghcr.io/neurekadev/warrden",
		"docker/setup-qemu-action@v4",
		"docker/setup-buildx-action@v4",
		"docker/login-action@v4",
		"docker/metadata-action@v6",
		"docker/build-push-action@v7",
		"actions/attest@v4",
		"type=raw,value=latest",
		"packages: write",
		"attestations: write",
		"artifact-metadata: write",
		"contents: write",
		"gh release create",
	} {
		if !strings.Contains(workflowText, want) {
			t.Errorf(".github/workflows/ci.yml missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"CONFIG_PATH=",
		"DATABASE_PATH=",
		"warden.db",
		"warrden-container-test-config:/config",
		"$CI_",
		"code.neureka.dev",
		"registry.neureka.dev",
	} {
		if strings.Contains(workflowText, forbidden) {
			t.Errorf(".github/workflows/ci.yml contains forbidden value %q", forbidden)
		}
	}
	if strings.Contains(workflowText, "$CI_PROJECT_DIR") {
		t.Error("container smoke tests must not bind runner-local paths through the host Docker daemon")
	}
	if _, err := os.Stat("../../.gitlab-ci.yml"); !os.IsNotExist(err) {
		t.Errorf(".gitlab-ci.yml must be removed, stat error: %v", err)
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
