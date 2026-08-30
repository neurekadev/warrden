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
		"image: ghcr.io/neurekadev/warrden:5",
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
	if strings.Contains(composeText, "TELEMETRY") {
		t.Error("compose.yaml must not configure TELEMETRY")
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
	if strings.Contains(environment, "TELEMETRY") {
		t.Error(".env.example must not document TELEMETRY")
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
		"actions/workflows/CI.yaml",
		"Download [`compose.yaml`](./compose.yaml) and [`.env.example`](./.env.example).",
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
	if !strings.Contains(readme, "`TELEMETRY=false`") {
		t.Error("README.md must document the telemetry opt-out")
	}
	lastSection := ""
	for _, line := range strings.Split(strings.TrimSpace(readme), "\n") {
		if strings.HasPrefix(line, "## ") {
			lastSection = line
		}
	}
	if lastSection != "## Telemetry" {
		t.Errorf("README.md final section=%q, want %q", lastSection, "## Telemetry")
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

	workflow := readRepositoryFile(t, "../../.github/workflows/CI.yaml")
	var workflowDocument yaml.Node
	if err := yaml.Unmarshal(workflow, &workflowDocument); err != nil {
		t.Fatalf(".github/workflows/CI.yaml: %v", err)
	}
	workflowText := string(workflow)
	for _, want := range []string{
		"name: CI",
		"pull_request:",
		"workflow_dispatch:",
		"permissions: {}",
		"cancel-in-progress: ${{ github.ref_type != 'tag' }}",
		"  lint:",
		"  unit-tests:",
		"  build:",
		"  publish-platforms:",
		"  publish:",
		"  release:",
		"actions/checkout@v7",
		"actions/setup-go@v7",
		"golangci/golangci-lint-action@v9",
		"version: v2.13.2",
		"CGO_ENABLED: 0",
		"go test ./...",
		"go build ./...",
		"docker compose config --quiet",
		"ubuntu-24.04-arm",
		"platform: linux/amd64",
		"platform: linux/arm64",
		"docker/setup-buildx-action@v4",
		"docker/login-action@v4",
		"docker/metadata-action@v6",
		"docker/build-push-action@v7",
		"actions/upload-artifact@v7",
		"actions/download-artifact@v8",
		"cache-from: type=gha,scope=ci-${{ matrix.component }}-${{ matrix.arch }}",
		"CACHE_PREFIX: ${{ github.event_name == 'pull_request' && 'ci' || 'publish' }}",
		"cache-from: type=gha,scope=${{ env.CACHE_PREFIX }}-${{ matrix.component }}-${{ matrix.arch }}",
		"provenance: mode=max",
		"sbom: true",
		"push-by-digest=true",
		"docker buildx imagetools create",
		"github.event.pull_request.head.repo.full_name == github.repository",
		"format('pr-{0}', github.event.pull_request.number)",
		"type=ref,event=pr",
		"type=edge",
		"type=semver,pattern={{version}}",
		"packages: write",
		"contents: write",
		"release-tag:",
		"release-notes-audience:",
		"  regenerate-release-notes:",
		"neurekadev/create-release-action@1",
		"secrets.INFERENCE_API_KEY",
	} {
		if !strings.Contains(workflowText, want) {
			t.Errorf(".github/workflows/CI.yaml missing %q", want)
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
		"go mod tidy -diff",
		"govulncheck",
		"go test -race",
		"docker/setup-qemu-action",
		"docker run",
		"pull_request_target:",
		"actions/attest",
		"id-token: write",
		"attestations: write",
		"gh release create",
		"push-to-registry:",
		"artifact-metadata: write",
	} {
		if strings.Contains(workflowText, forbidden) {
			t.Errorf(".github/workflows/CI.yaml contains forbidden value %q", forbidden)
		}
	}
	if _, err := os.Stat("../../.github/workflows/ci.yml"); !os.IsNotExist(err) {
		t.Errorf("legacy .github/workflows/ci.yml must be removed, stat error: %v", err)
	}

	dependabot := readRepositoryFile(t, "../../.github/dependabot.yml")
	var dependabotDocument struct {
		Version int `yaml:"version"`
		Updates []struct {
			PackageEcosystem string `yaml:"package-ecosystem"`
			Directory        string `yaml:"directory"`
			Schedule         struct {
				Interval string `yaml:"interval"`
				Day      string `yaml:"day"`
				Time     string `yaml:"time"`
				Timezone string `yaml:"timezone"`
			} `yaml:"schedule"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal(dependabot, &dependabotDocument); err != nil {
		t.Fatalf(".github/dependabot.yml: %v", err)
	}
	if dependabotDocument.Version != 2 {
		t.Errorf(".github/dependabot.yml version=%d, want 2", dependabotDocument.Version)
	}
	wantEcosystems := map[string]bool{
		"docker":         true,
		"github-actions": true,
		"gomod":          true,
	}
	seenEcosystems := make(map[string]bool, len(wantEcosystems))
	for _, update := range dependabotDocument.Updates {
		if !wantEcosystems[update.PackageEcosystem] {
			t.Errorf(".github/dependabot.yml contains unexpected ecosystem %q", update.PackageEcosystem)
			continue
		}
		if seenEcosystems[update.PackageEcosystem] {
			t.Errorf(".github/dependabot.yml contains duplicate ecosystem %q", update.PackageEcosystem)
		}
		seenEcosystems[update.PackageEcosystem] = true
		if update.Directory != "/" {
			t.Errorf("dependabot %s directory=%q, want %q", update.PackageEcosystem, update.Directory, "/")
		}
		if update.Schedule.Interval != "weekly" || update.Schedule.Day != "monday" || update.Schedule.Time != "07:00" || update.Schedule.Timezone != "America/Los_Angeles" {
			t.Errorf("dependabot %s schedule=%+v, want Monday at 07:00 America/Los_Angeles", update.PackageEcosystem, update.Schedule)
		}
	}
	for ecosystem := range wantEcosystems {
		if !seenEcosystems[ecosystem] {
			t.Errorf(".github/dependabot.yml missing ecosystem %q", ecosystem)
		}
	}
	if got := len(dependabotDocument.Updates); got != len(wantEcosystems) {
		t.Errorf(".github/dependabot.yml update count=%d, want %d", got, len(wantEcosystems))
	}
	if _, err := os.Stat("../../renovate.json"); !os.IsNotExist(err) {
		t.Errorf("renovate.json must be removed, stat error: %v", err)
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
