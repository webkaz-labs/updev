package nativeaudit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/webkaz-labs/updev/internal/securityadvisory"
)

func TestProjectPythonSitePackagesPath(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, ".venv", "lib", "python3.12", "site-packages")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ProjectPythonSitePackagesPath(root); got != want {
		t.Fatalf("expected site-packages %q, got %q", want, got)
	}
}

func TestProjectPythonRequirementPaths(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{
		filepath.Join(root, "requirements.txt"),
		filepath.Join(root, "dev-requirements.txt"),
		filepath.Join(root, "requirements", "extra.txt"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("demo\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := ProjectPythonRequirementPaths(root)
	if len(got) != 3 {
		t.Fatalf("expected three requirement files, got %#v", got)
	}
	if got[0] != filepath.Join(root, "dev-requirements.txt") {
		t.Fatalf("expected sorted requirement paths, got %#v", got)
	}
}

func TestProjectPythonLockedAuditTargetPrefersPyprojectThenPylock(t *testing.T) {
	root := t.TempDir()
	pylock := filepath.Join(root, "pylock.dev.toml")
	if err := os.WriteFile(pylock, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ProjectPythonLockedAuditTarget(root); got != pylock {
		t.Fatalf("expected pylock target %q, got %q", pylock, got)
	}
	pyproject := filepath.Join(root, "pyproject.toml")
	if err := os.WriteFile(pyproject, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ProjectPythonLockedAuditTarget(root); got != pyproject {
		t.Fatalf("expected pyproject target %q, got %q", pyproject, got)
	}
}

func TestProjectTargetHelpers(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"go.mod", "Demo.sln", "Demo.csproj", "pom.xml", "build.gradle.kts"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := ProjectGoModulePath(root); got != filepath.Join(root, "go.mod") {
		t.Fatalf("expected go module path, got %q", got)
	}
	if got := ProjectDotnetTargets(root); len(got) != 2 {
		t.Fatalf("expected two dotnet targets, got %#v", got)
	}
	if got := ProjectMavenTargets(root); len(got) != 2 {
		t.Fatalf("expected two maven targets, got %#v", got)
	}
	if got := ProjectLockfilePath(root, "missing.lock", "go.mod"); got != filepath.Join(root, "go.mod") {
		t.Fatalf("expected lockfile fallback, got %q", got)
	}
}

func TestCargoAuditBinaryPathsUsesOnPathEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin", "fd")
	got := CargoAuditBinaryPaths([]securityadvisory.Package{{
		Ecosystem:  "crates.io",
		Package:    "fd-find",
		BinaryPath: path,
		PathState:  "on-path",
	}})
	if len(got) != 1 || got[0] != path {
		t.Fatalf("expected on-path cargo binary, got %#v", got)
	}
}

func TestCargoBinaryCandidatesUsesKnownBinaryNames(t *testing.T) {
	got := CargoBinaryCandidates("fd-find")
	if len(got) != 2 || got[0] != "fd" || got[1] != "fd-find" {
		t.Fatalf("expected fd-find candidates, got %#v", got)
	}
	if got := CargoBinaryCandidates("../tool"); got != nil {
		t.Fatalf("expected unsafe crate to be rejected, got %#v", got)
	}
}

func TestPipxAuditPathsRejectsUnsafePackageParts(t *testing.T) {
	if got := PipxAuditPaths([]securityadvisory.Package{{Ecosystem: "PyPI", Package: "../frogmouth", Version: "0.9.2"}}); len(got) != 0 {
		t.Fatalf("expected unsafe package to be rejected, got %#v", got)
	}
	if got := PipxAuditPaths([]securityadvisory.Package{{Ecosystem: "PyPI", Package: "frogmouth", Version: "../0.9.2"}}); len(got) != 0 {
		t.Fatalf("expected unsafe version to be rejected, got %#v", got)
	}
}
