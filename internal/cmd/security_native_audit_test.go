package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/nativeaudit"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

func TestPrintSecurityTextIncludesNativeAuditAttention(t *testing.T) {
	var buffer bytes.Buffer
	printSecurityText(&buffer, securityReport{
		Status:  plan.StatusOK,
		Root:    "/repo",
		Sources: []string{"provider-native-audit"},
		Audits: []nativeAudit{{
			Ecosystem: "npm",
			Tool:      "npm",
			Status:    plan.StatusUnavailable,
			Decision:  "review",
			Reason:    "npm audit does not support globals",
			Error:     "EAUDITGLOBAL",
		}},
	}, false)
	got := buffer.String()
	if !strings.Contains(got, "native audits") || !strings.Contains(got, "unavailable") || !strings.Contains(got, "globals") || !strings.Contains(got, "EAUDITGLOBAL") {
		t.Fatalf("expected native audit attention in text output, got %q", got)
	}
}

func TestRunNPMNativeAuditReportsGlobalUnsupported(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"error":{"code":"EAUDITGLOBAL","summary":"npm audit does not support globals"}}`,
	}}
	audit := runNPMNativeAudit(context.Background(), fake)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Reason, "globals") {
		t.Fatalf("expected unsupported npm global audit, got %#v", audit)
	}
}

func TestRunNPMNativeAuditReportsVulnerabilities(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `{
  "vulnerabilities": {
    "left-pad": {"severity":"high"}
  },
  "metadata": {"vulnerabilities": {"high": 1, "total": 1}}
}`,
	}}
	audit := runNPMNativeAudit(context.Background(), fake)
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 || audit.Vulnerabilities == nil || audit.Vulnerabilities.High != 1 {
		t.Fatalf("expected held npm audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "npm", "npm")
}

func TestRunNPMLockfileNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "package-lock.json")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `{
  "vulnerabilities": {
    "left-pad": {"severity":"high"}
  },
  "metadata": {"vulnerabilities": {"high": 1, "total": 1}}
}`,
	}}
	audit := runNPMLockfileNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Tool != "npm" || audit.Target != lockfile {
		t.Fatalf("expected npm project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 || audit.Vulnerabilities == nil || audit.Vulnerabilities.High != 1 {
		t.Fatalf("expected held npm lockfile audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "npm", "npm")
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--prefix") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected npm --prefix audit call, got %#v", fake.calls)
	}
}

func TestRunPNPMLockfileNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "pnpm-lock.yaml")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Stdout: `{
  "vulnerabilities": {
    "left-pad": {"severity":"high"}
  },
  "metadata": {"vulnerabilities": {"high": 1, "total": 1}}
}`,
	}}
	audit := runPNPMLockfileNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Tool != "pnpm" || audit.Target != lockfile {
		t.Fatalf("expected pnpm project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 || audit.Vulnerabilities == nil || audit.Vulnerabilities.High != 1 {
		t.Fatalf("expected held pnpm audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "pnpm", "npm")
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--dir") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected pnpm --dir audit call, got %#v", fake.calls)
	}
}

func TestRunBunLockfileNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "bun.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"advisories":[{"id":"GHSA-test"}]}`,
	}}
	audit := runBunLockfileNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Tool != "bun" || audit.Target != lockfile {
		t.Fatalf("expected bun project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 1 {
		t.Fatalf("expected held bun audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "bun", "npm")
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--cwd") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected bun --cwd audit call, got %#v", fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsOnlyMatchingEcosystem(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	root := t.TempDir()
	audits := nativeAuditsFromPackages(context.Background(), fake, []securityPackage{{Ecosystem: "npm"}}, securityOptions{root: root, ecosystem: "pypi"})
	if len(audits) != 0 {
		t.Fatalf("expected npm native audit to be skipped by ecosystem filter, got %#v", audits)
	}
	audits = nativeAuditsFromPackages(context.Background(), fake, []securityPackage{{Ecosystem: "npm"}}, securityOptions{root: root, ecosystem: "npm"})
	if len(audits) != 1 || audits[0].Status != plan.StatusOK {
		t.Fatalf("expected npm native audit, got %#v", audits)
	}
}

func TestNativeAuditsFromPackagesRunsProjectLockfileAudits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "bun.lock"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "npm"})
	if len(audits) != 3 {
		t.Fatalf("expected npm, pnpm, and bun project audits, got %#v", audits)
	}
	if audits[0].Tool != "npm" || audits[0].Target != filepath.Join(root, "package-lock.json") {
		t.Fatalf("expected npm lockfile audit, got %#v", audits)
	}
	if audits[1].Tool != "pnpm" || audits[1].Target != filepath.Join(root, "pnpm-lock.yaml") {
		t.Fatalf("expected pnpm lockfile audit, got %#v", audits)
	}
	if audits[2].Tool != "bun" || audits[2].Target != filepath.Join(root, "bun.lock") {
		t.Fatalf("expected bun lockfile audit, got %#v", audits)
	}
	if len(fake.calls) != 3 {
		t.Fatalf("expected three project audit calls, got %#v", fake.calls)
	}
	focusedFake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	focusedAudits := nativeAuditsFromPackages(context.Background(), focusedFake, nil, securityOptions{root: root, ecosystem: "npm", provider: "brew"})
	if len(focusedAudits) != 0 || len(focusedFake.calls) != 0 {
		t.Fatalf("expected brew provider filter to skip project audits, got audits=%#v calls=%#v", focusedAudits, focusedFake.calls)
	}
	projectFake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	projectAudits := nativeAuditsFromPackages(context.Background(), projectFake, nil, securityOptions{root: root, ecosystem: "npm", provider: "project"})
	if len(projectAudits) != 3 || len(projectFake.calls) != 3 {
		t.Fatalf("expected project provider filter to run project audits, got audits=%#v calls=%#v", projectAudits, projectFake.calls)
	}
}

func TestRunCargoNativeAuditReportsMissingCommand(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   101,
		Stderr: "error: no such command: `audit`",
	}}
	audit := runCargoNativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", BinaryPath: "/tmp/fd", PathState: "on-path"}})
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "no such command") {
		t.Fatalf("expected unavailable cargo audit, got %#v", audit)
	}
}

func TestRunCargoNativeAuditReportsVulnerabilities(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"vulnerabilities":{"found":true,"count":2,"list":[{},{}]}}`,
	}}
	audit := runCargoNativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", BinaryPath: "/tmp/fd", PathState: "on-path"}})
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held cargo audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "cargo-audit", "crates.io")
}

func TestRunCargoNativeAuditReportsMissingBinaryContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CARGO_HOME", filepath.Join(home, ".cargo"))
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audit := runCargoNativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", Package: "fd-find"}})
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || len(fake.calls) != 0 {
		t.Fatalf("expected unavailable cargo audit without running command, got %#v calls=%#v", audit, fake.calls)
	}
}

func TestRunCargoNativeAuditUsesCargoHomeBinary(t *testing.T) {
	home := t.TempDir()
	cargoBin := filepath.Join(home, "cargo-home", "bin")
	if err := os.MkdirAll(cargoBin, 0o700); err != nil {
		t.Fatal(err)
	}
	fdPath := filepath.Join(cargoBin, "fd")
	if err := os.WriteFile(fdPath, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(home, "empty-home"))
	t.Setenv("CARGO_HOME", filepath.Join(home, "cargo-home"))
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audit := runCargoNativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", Package: "fd-find", BinaryName: "fd,fd-find", PathState: "not-found"}})
	if audit.Status != plan.StatusOK || len(fake.calls) != 1 {
		t.Fatalf("expected cargo audit to use CARGO_HOME binary, got %#v calls=%#v", audit, fake.calls)
	}
	if !containsString(fake.calls[0], fdPath) {
		t.Fatalf("expected cargo audit command to include %s, got %#v", fdPath, fake.calls[0])
	}
}

func TestRunCargoProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "Cargo.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"vulnerabilities":{"found":true,"count":2,"list":[{},{}]}}`,
	}}
	audit := runCargoProjectNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Ecosystem != "crates.io" || audit.Tool != "cargo-audit" || audit.Target != lockfile {
		t.Fatalf("expected Cargo project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held Cargo project audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "cargo-audit", "crates.io")
	if len(fake.calls) != 1 || fake.calls[0][0] != "bash" || !containsString(fake.calls[0], root) {
		t.Fatalf("expected bash-wrapped cargo audit call, got %#v", fake.calls)
	}
}

func TestRunCargoProjectNativeAuditReportsMissingCommand(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "Cargo.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   101,
		Stderr: "error: no such command: `audit`",
	}}
	audit := runCargoProjectNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "no such command") {
		t.Fatalf("expected unavailable Cargo project audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsCargoEcosystem(t *testing.T) {
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, []securityPackage{{Ecosystem: "crates.io", BinaryPath: "/tmp/fd", PathState: "on-path"}}, securityOptions{ecosystem: "crates.io"})
	if len(audits) != 1 || audits[0].Ecosystem != "crates.io" || audits[0].Status != plan.StatusOK {
		t.Fatalf("expected cargo native audit, got %#v", audits)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "bin") || !containsString(fake.calls[0], "/tmp/fd") {
		t.Fatalf("expected cargo audit bin call, got %#v", fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsCargoProjectAudit(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "Cargo.lock")
	if err := os.WriteFile(lockfile, []byte("[[package]]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "crates.io"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Tool != "cargo-audit" || audits[0].Target != lockfile {
		t.Fatalf("expected Cargo project native audit, got %#v", audits)
	}
}

func TestRunPyPINativeAuditReportsMissingCommand(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	makePipxSitePackages(t, miseDir, "frogmouth", "0.9.2")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runPyPINativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "0.9.2"}})
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable pip-audit, got %#v", audit)
	}
}

func TestRunPyPINativeAuditReportsVulnerabilities(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	sitePackages := makePipxSitePackages(t, miseDir, "frogmouth", "0.9.2")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"dependencies":[{"name":"frogmouth","version":"0.9.2","vulns":[{"id":"PYSEC-test"},{"id":"GHSA-test"}]}]}`,
	}}
	audit := runPyPINativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "0.9.2"}})
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held pip-audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "pip-audit", "PyPI")
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--path") || !containsString(fake.calls[0], sitePackages) {
		t.Fatalf("expected pip-audit path call, got %#v", fake.calls)
	}
}

func TestRunPyPINativeAuditReportsMissingPipxContext(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audit := runPyPINativeAudit(context.Background(), fake, []securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "0.9.2"}})
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || len(fake.calls) != 0 {
		t.Fatalf("expected unavailable pip-audit without paths, got %#v calls=%#v", audit, fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsPyPIEcosystem(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	makePipxSitePackages(t, miseDir, "frogmouth", "0.9.2")
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"dependencies":[]}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, []securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "0.9.2"}}, securityOptions{root: t.TempDir(), ecosystem: "pypi"})
	if len(audits) != 1 || audits[0].Ecosystem != "PyPI" || audits[0].Status != plan.StatusOK {
		t.Fatalf("expected PyPI native audit, got %#v", audits)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "pip-audit" {
		t.Fatalf("expected pip-audit call, got %#v", fake.calls)
	}
}

func TestRunNativeAuditTasksPreservesTaskOrder(t *testing.T) {
	audits := runNativeAuditTasks([]func() nativeAudit{
		func() nativeAudit {
			time.Sleep(5 * time.Millisecond)
			return nativeAudit{Tool: "slow"}
		},
		func() nativeAudit {
			return nativeAudit{Tool: "fast"}
		},
	})
	if len(audits) != 2 || audits[0].Tool != "slow" || audits[1].Tool != "fast" {
		t.Fatalf("expected native audit task order to be stable, got %#v", audits)
	}
}

func TestRunPythonProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	sitePackages := filepath.Join(t.TempDir(), ".venv", "lib", "python3.12", "site-packages")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"dependencies":[{"name":"requests","version":"2.0.0","vulns":[{"id":"PYSEC-test"}]}]}`,
	}}
	audit := runPythonProjectNativeAudit(context.Background(), fake, sitePackages)
	if audit.Provider != "project" || audit.Ecosystem != "PyPI" || audit.Tool != "pip-audit" || audit.Target != sitePackages {
		t.Fatalf("expected Python project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 {
		t.Fatalf("expected held Python project audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "pip-audit", "PyPI")
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--path") || !containsString(fake.calls[0], sitePackages) {
		t.Fatalf("expected pip-audit --path call, got %#v", fake.calls)
	}
}

func TestRunPythonRequirementsNativeAuditReportsVulnerabilities(t *testing.T) {
	requirements := filepath.Join(t.TempDir(), "requirements.txt")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"dependencies":[{"name":"requests","version":"2.0.0","vulns":[{"id":"PYSEC-test"}]}]}`,
	}}
	audit := runPythonRequirementsNativeAudit(context.Background(), fake, requirements)
	if audit.Provider != "project" || audit.Ecosystem != "PyPI" || audit.Tool != "pip-audit" || audit.Target != requirements {
		t.Fatalf("expected Python requirements audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 {
		t.Fatalf("expected held Python requirements audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--requirement") || !containsString(fake.calls[0], requirements) {
		t.Fatalf("expected pip-audit --requirement call, got %#v", fake.calls)
	}
}

func TestRunPythonLockedProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	pyproject := filepath.Join(root, "pyproject.toml")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"dependencies":[{"name":"requests","version":"2.0.0","vulns":[{"id":"PYSEC-test"}]}]}`,
	}}
	audit := runPythonLockedProjectNativeAudit(context.Background(), fake, root, pyproject)
	if audit.Provider != "project" || audit.Ecosystem != "PyPI" || audit.Tool != "pip-audit" || audit.Target != pyproject {
		t.Fatalf("expected Python locked project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 1 {
		t.Fatalf("expected held Python locked project audit, got %#v", audit)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--locked") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected pip-audit --locked project call, got %#v", fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsPythonProjectAudit(t *testing.T) {
	root := t.TempDir()
	sitePackages := filepath.Join(root, ".venv", "lib", "python3.12", "site-packages")
	if err := os.MkdirAll(sitePackages, 0o700); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"dependencies":[]}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "pypi"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Target != sitePackages {
		t.Fatalf("expected Python project native audit, got %#v", audits)
	}
}

func TestNativeAuditsFromPackagesRunsPythonRequirementsAudits(t *testing.T) {
	root := t.TempDir()
	requirements := filepath.Join(root, "requirements.txt")
	devRequirements := filepath.Join(root, "requirements", "dev.txt")
	if err := os.WriteFile(requirements, []byte("requests==2.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(devRequirements), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(devRequirements, []byte("pytest==8.0.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"dependencies":[]}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "pypi"})
	if len(audits) != 2 {
		t.Fatalf("expected two Python requirements audits, got %#v", audits)
	}
	if audits[0].Target != requirements || audits[1].Target != devRequirements {
		t.Fatalf("expected sorted requirements audit targets, got %#v", audits)
	}
	if len(fake.calls) != 2 || !containsString(fake.calls[0], "--requirement") || !containsString(fake.calls[1], "--requirement") {
		t.Fatalf("expected pip-audit requirement calls, got %#v", fake.calls)
	}
}

func TestNativeAuditsFromPackagesRunsPythonLockedProjectAudit(t *testing.T) {
	root := t.TempDir()
	pyproject := filepath.Join(root, "pyproject.toml")
	if err := os.WriteFile(pyproject, []byte("[project]\nname = \"demo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{"dependencies":[]}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "pypi"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Target != pyproject {
		t.Fatalf("expected Python locked project audit, got %#v", audits)
	}
	if len(fake.calls) != 1 || !containsString(fake.calls[0], "--locked") || !containsString(fake.calls[0], root) {
		t.Fatalf("expected pip-audit --locked project call, got %#v", fake.calls)
	}
}

func TestProjectPythonLockedAuditTargetUsesPylock(t *testing.T) {
	root := t.TempDir()
	pylock := filepath.Join(root, "pylock.dev.toml")
	if err := os.WriteFile(pylock, []byte("[[packages]]\nname = \"demo\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := projectPythonLockedAuditTarget(root); got != pylock {
		t.Fatalf("expected pylock target, got %q", got)
	}
}

func TestRunGoProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "go.mod")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 3,
		Stdout: `{"finding":{"osv":"GO-2026-0001","fixed_version":"v1.2.3"}}
{"finding":{"osv":"GO-2026-0001"}}
{"finding":{"osv":"GO-2026-0002"}}`,
	}}
	audit := runGoProjectNativeAudit(context.Background(), fake, root, module)
	if audit.Provider != "project" || audit.Ecosystem != "Go" || audit.Tool != "govulncheck" || audit.Target != module {
		t.Fatalf("expected Go project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held govulncheck audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "govulncheck", "Go")
	if len(fake.calls) != 1 || fake.calls[0][0] != "govulncheck" || !containsString(fake.calls[0], "-format=json") || !containsString(fake.calls[0], filepath.Join(root, "...")) {
		t.Fatalf("expected govulncheck JSON call, got %#v", fake.calls)
	}
}

func TestRunGoProjectNativeAuditReportsMissingCommand(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "go.mod")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runGoProjectNativeAudit(context.Background(), fake, root, module)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable govulncheck audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsGoProjectAudit(t *testing.T) {
	root := t.TempDir()
	module := filepath.Join(root, "go.mod")
	if err := os.WriteFile(module, []byte("module example.com/test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "go"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Tool != "govulncheck" || audits[0].Target != module {
		t.Fatalf("expected Go project native audit, got %#v", audits)
	}
}

func TestRunComposerProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "composer.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"advisories":{"vendor/package":[{"advisoryId":"PKSA-test-1"},{"advisoryId":"PKSA-test-2"}]}}`,
	}}
	audit := runComposerProjectNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Provider != "project" || audit.Ecosystem != "Packagist" || audit.Tool != "composer" || audit.Target != lockfile {
		t.Fatalf("expected Composer project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held Composer audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "composer", "Packagist")
	if len(fake.calls) != 1 || fake.calls[0][0] != "composer" || !containsString(fake.calls[0], "--working-dir") || !containsString(fake.calls[0], root) || !containsString(fake.calls[0], "--locked") {
		t.Fatalf("expected composer audit call, got %#v", fake.calls)
	}
}

func TestRunComposerProjectNativeAuditReportsMissingCommand(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "composer.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runComposerProjectNativeAudit(context.Background(), fake, root, lockfile)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable Composer audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsComposerProjectAudit(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "composer.lock")
	if err := os.WriteFile(lockfile, []byte(`{"packages":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "packagist"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Tool != "composer" || audits[0].Target != lockfile {
		t.Fatalf("expected Composer project native audit, got %#v", audits)
	}
}

func TestRunBundlerProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	lockfile := filepath.Join(t.TempDir(), "Gemfile.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"results":[{"gem":"rails","advisory":{"id":"CVE-test-1"}},{"gem":"rack","advisory":{"id":"CVE-test-2"}}]}`,
	}}
	audit := runBundlerProjectNativeAudit(context.Background(), fake, lockfile)
	if audit.Provider != "project" || audit.Ecosystem != "RubyGems" || audit.Tool != "bundle-audit" || audit.Target != lockfile {
		t.Fatalf("expected Bundler project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 2 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 2 {
		t.Fatalf("expected held bundle-audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "bundle-audit", "RubyGems")
	if len(fake.calls) != 1 || fake.calls[0][0] != "bundle-audit" || !containsString(fake.calls[0], "--gemfile") || !containsString(fake.calls[0], lockfile) {
		t.Fatalf("expected bundle-audit JSON call, got %#v", fake.calls)
	}
}

func TestRunBundlerProjectNativeAuditReportsMissingCommand(t *testing.T) {
	lockfile := filepath.Join(t.TempDir(), "Gemfile.lock")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runBundlerProjectNativeAudit(context.Background(), fake, lockfile)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable Bundler audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsBundlerProjectAudit(t *testing.T) {
	root := t.TempDir()
	lockfile := filepath.Join(root, "Gemfile.lock")
	if err := os.WriteFile(lockfile, []byte("GEM\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "rubygems"})
	if len(audits) != 1 || audits[0].Provider != "project" || audits[0].Tool != "bundle-audit" || audits[0].Target != lockfile {
		t.Fatalf("expected Bundler project native audit, got %#v", audits)
	}
}

func TestRunDotnetProjectNativeAuditReportsVulnerabilities(t *testing.T) {
	target := filepath.Join(t.TempDir(), "Demo.sln")
	fake := &fakeCommandRunner{result: runner.Result{
		Code:   1,
		Stdout: `{"projects":[{"frameworks":[{"topLevelPackages":[{"vulnerabilities":[{"severity":"High"}]}],"transitivePackages":[{"vulnerabilities":[{"severity":"Moderate"},{"severity":"Low"}]}]}]}]}`,
	}}
	audit := runDotnetProjectNativeAudit(context.Background(), fake, target)
	if audit.Provider != "project" || audit.Ecosystem != "NuGet" || audit.Tool != "dotnet" || audit.Target != target {
		t.Fatalf("expected .NET project audit identity, got %#v", audit)
	}
	if audit.Status != plan.StatusHeld || audit.Decision != "hold" || audit.AdvisoryCount != 3 || audit.Vulnerabilities == nil || audit.Vulnerabilities.Total != 3 {
		t.Fatalf("expected held dotnet audit, got %#v", audit)
	}
	assertNativeAuditVulnerabilityReason(t, audit, "dotnet", "NuGet")
	if len(fake.calls) != 1 || fake.calls[0][0] != "dotnet" || !containsString(fake.calls[0], "--include-transitive") || !containsString(fake.calls[0], "--vulnerable") || !containsString(fake.calls[0], target) {
		t.Fatalf("expected dotnet package list call, got %#v", fake.calls)
	}
}

func TestLocalizedNativeAuditReasonUsesReasonCode(t *testing.T) {
	withDefaultLanguageForTest(t, "ja")
	audit := nativeAudit{
		Tool:       "pip-audit",
		Ecosystem:  "PyPI",
		Reason:     "pip-audit reported vulnerabilities",
		ReasonCode: securityreason.NativeAuditVulnerability,
		ReasonArgs: map[string]string{"tool": "pip-audit", "ecosystem": "PyPI"},
	}
	got := localizedNativeAuditReason(audit)
	want := "pip-audit が PyPI の native audit で脆弱性を検出しました"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func assertNativeAuditVulnerabilityReason(t *testing.T, audit nativeAudit, tool string, ecosystem string) {
	t.Helper()
	if audit.ReasonCode != securityreason.NativeAuditVulnerability {
		t.Fatalf("expected native audit vulnerability reason code, got %#v", audit)
	}
	if audit.ReasonArgs["tool"] != tool || audit.ReasonArgs["ecosystem"] != ecosystem {
		t.Fatalf("expected native audit reason args tool=%q ecosystem=%q, got %#v", tool, ecosystem, audit.ReasonArgs)
	}
	if !strings.Contains(audit.Reason, "reported vulnerabilities") {
		t.Fatalf("expected compatible reason text, got %#v", audit)
	}
}

func TestRunDotnetProjectNativeAuditReportsMissingCommand(t *testing.T) {
	target := filepath.Join(t.TempDir(), "Demo.csproj")
	fake := &fakeCommandRunner{result: runner.Result{
		Code: 1,
		Err:  os.ErrNotExist,
	}}
	audit := runDotnetProjectNativeAudit(context.Background(), fake, target)
	if audit.Status != plan.StatusUnavailable || audit.Decision != "review" || !strings.Contains(audit.Error, "file does not exist") {
		t.Fatalf("expected unavailable dotnet audit, got %#v", audit)
	}
}

func TestNativeAuditsFromPackagesRunsDotnetProjectAudit(t *testing.T) {
	root := t.TempDir()
	solution := filepath.Join(root, "Demo.sln")
	project := filepath.Join(root, "Demo.csproj")
	if err := os.WriteFile(solution, []byte("Microsoft Visual Studio Solution File\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(project, []byte("<Project />\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "nuget"})
	if len(audits) != 2 || audits[0].Target != project || audits[1].Target != solution {
		t.Fatalf("expected sorted .NET project native audits, got %#v", audits)
	}
}

func TestNativeAuditsFromPackagesReportsMavenProjectAuditUnavailable(t *testing.T) {
	root := t.TempDir()
	pom := filepath.Join(root, "pom.xml")
	gradle := filepath.Join(root, "build.gradle.kts")
	if err := os.WriteFile(pom, []byte("<project />\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gradle, []byte("plugins {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeCommandRunner{result: runner.Result{Stdout: `{}`}}
	audits := nativeAuditsFromPackages(context.Background(), fake, nil, securityOptions{root: root, ecosystem: "maven"})
	if len(audits) != 2 || audits[0].Provider != "project" || audits[0].Ecosystem != "Maven" || audits[0].Status != plan.StatusUnavailable || audits[0].Decision != "review" {
		t.Fatalf("expected unavailable Maven project audits, got %#v", audits)
	}
	if audits[0].Target != gradle || audits[1].Target != pom {
		t.Fatalf("expected sorted Maven targets, got %#v", audits)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("expected Maven unavailable audit not to run a command, got %#v", fake.calls)
	}
}

func TestPipxAuditPathsRejectUnsafePackageName(t *testing.T) {
	miseDir := t.TempDir()
	t.Setenv("MISE_DATA_DIR", miseDir)
	makePipxSitePackages(t, miseDir, "frogmouth", "0.9.2")
	paths := nativeaudit.PipxAuditPaths([]securityPackage{{Ecosystem: "PyPI", Package: "../frogmouth", Version: "0.9.2"}})
	if len(paths) != 0 {
		t.Fatalf("expected unsafe package name to be rejected, got %#v", paths)
	}
	paths = nativeaudit.PipxAuditPaths([]securityPackage{{Ecosystem: "PyPI", Package: "frogmouth", Version: "../0.9.2"}})
	if len(paths) != 0 {
		t.Fatalf("expected unsafe package version to be rejected, got %#v", paths)
	}
}

func makePipxSitePackages(t *testing.T, miseDir string, pkg string, version string) string {
	t.Helper()
	path := filepath.Join(miseDir, "installs", "pipx-"+pkg, version, pkg, "lib", "python3.13", "site-packages")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
