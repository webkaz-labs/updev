package cmd

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/updatereason"
)

func TestRunUpdateAutoMiseBumpRunsScopedSafeCandidates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UPDEV_CONFIG", filepath.Join(t.TempDir(), "missing-config.toml"))
	t.Setenv("UPDEV_MISE_MIN_RELEASE_AGE_DAYS", "0")
	root := t.TempDir()
	safeBumpJSON := `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"0.60.1","latest":"0.60.1"}}`
	preflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"}, "\x00")
	applyKey := strings.Join([]string{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"}, "\x00")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_INSTALL_FROM_API=1", "brew", "outdated", "--json=v2", "--greedy"}, "\x00"): {
			Stdout: `{"formulae":[],"casks":[]}`,
		},
		strings.Join([]string{"mise", "settings", "ls", "--json-extended", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: safeBumpJSON,
		},
		strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: `{}`,
		},
		strings.Join([]string{"brew", "update"}, "\x00"): {
			Stdout: "Already up-to-date.",
		},
		strings.Join([]string{"mise", "upgrade"}, "\x00"): {
			Stdout: "All tools are up to date",
		},
		strings.Join([]string{"mise", "prune"}, "\x00"): {Stdout: "mise pruned configuration links"},
		preflightKey: {Stdout: "Would bump github:openai/codex"},
		applyKey:     {Stdout: "github:openai/codex 0.60.0 -> 0.60.1"},
	}}
	code := runUpdate(updateOptions{format: "text", root: root, security: "strict", miseBumpMode: "auto", noTUI: true}, fake)
	if code != 0 {
		t.Fatalf("expected successful auto bump update, got %d", code)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(preflightKey, "\x00")) {
		t.Fatalf("expected scoped dry-run bump command, calls=%#v", fake.calls)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(applyKey, "\x00")) {
		t.Fatalf("expected scoped apply bump command, calls=%#v", fake.calls)
	}
	for _, call := range fake.calls {
		if len(call) == 3 && call[0] == "mise" && call[1] == "upgrade" && call[2] == "--bump" {
			t.Fatalf("unscoped mise bump must not run, calls=%#v", fake.calls)
		}
	}
	entry, ok := loadLastUpdateReport()
	if !ok {
		t.Fatal("expected last update report to be saved")
	}
	var bumpStep updateStep
	for _, step := range entry.Report.Steps {
		if step.Name == miseBumpProvider {
			bumpStep = step
			break
		}
	}
	if bumpStep.Status != plan.StatusOK || len(bumpStep.Updated) != 1 || !strings.Contains(bumpStep.Updated[0], "github:openai/codex") {
		t.Fatalf("expected successful bump step in report, got %#v", bumpStep)
	}
	if !updateStepHasCommands(bumpStep, [][]string{
		{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"},
		{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"},
	}) {
		t.Fatalf("expected bump report to include preflight and apply commands, got %#v", bumpStep.Commands)
	}
}

func TestRunMiseBumpAutoDryRunShowsWouldUpdateCandidates(t *testing.T) {
	step, ok := runMiseBumpUpdateStep(context.Background(), &fakeCommandRunner{}, updateOptions{root: "/repo", security: "strict", miseBumpMode: "auto", dryRun: true}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{{
			Provider:          "mise",
			Kind:              "tool",
			Name:              "github:openai/codex",
			InstalledVersions: []string{"0.60.0"},
			CurrentVersion:    "0.60.1",
			Decision:          "allow",
			Source:            miseBumpSource,
		}},
	}}, false)
	if !ok || len(step.Updated) != 1 || len(step.SkippedItems) != 0 || !strings.HasPrefix(step.Updated[0], "would bump ") {
		t.Fatalf("expected dry-run auto bump to expose would-update row, ok=%v step=%#v", ok, step)
	}
	rows := updateOutcomeRows(updateReport{DryRun: true, Steps: []updateStep{step}}, 10, false)
	if len(rows) != 1 || rows[0][0] != "would" || !strings.Contains(rows[0][2], "github:openai/codex") {
		t.Fatalf("expected dry-run outcome to render as would-update, got %#v", rows)
	}
}

func TestRunMiseBumpOffModeDoesNotCreateUpdateStep(t *testing.T) {
	fake := &fakeCommandRunner{}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: "/repo", security: "strict", miseBumpMode: "off"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{{
			Provider:          "mise",
			Kind:              "tool",
			Name:              "github:openai/codex",
			InstalledVersions: []string{"0.60.0"},
			CurrentVersion:    "0.60.1",
			Decision:          "allow",
			Source:            miseBumpSource,
		}},
	}}, false)
	if ok || step.Name != "" || len(fake.calls) != 0 {
		t.Fatalf("expected off mode to skip mise-bump step without command calls, ok=%v step=%#v calls=%#v", ok, step, fake.calls)
	}
}

func TestRunMiseBumpManualModeKeepsAllCandidatesReviewOnly(t *testing.T) {
	safe := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	unsafe := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "npm:@google/gemini-cli",
		InstalledVersions: []string{"0.42.0"},
		CurrentVersion:    "0.46.0",
		Decision:          "review",
		Source:            miseBumpSource,
	}
	fake := &fakeCommandRunner{}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: "/repo", security: "strict", miseBumpMode: "manual"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusHeld,
		Findings: []safetyFinding{safe, unsafe},
	}}, false)
	if !ok || step.Status != plan.StatusDrift || !step.Skipped || len(step.Updated) != 0 || len(step.SkippedItems) != 2 || len(fake.calls) != 0 {
		t.Fatalf("expected manual mode to expose all candidates without applying, ok=%v step=%#v calls=%#v", ok, step, fake.calls)
	}
	if !strings.Contains(strings.Join(step.SkippedItems, "\n"), "github:openai/codex") || !strings.Contains(strings.Join(step.SkippedItems, "\n"), "npm:@google/gemini-cli") {
		t.Fatalf("expected manual mode skipped items to include safe and review candidates, got %#v", step.SkippedItems)
	}
}

func TestRunMiseBumpSafeModeKeepsSafeCandidatesConfirmationOnly(t *testing.T) {
	safe := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	fake := &fakeCommandRunner{}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: "/repo", security: "strict", miseBumpMode: "safe"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{safe},
	}}, false)
	if !ok || step.Status != plan.StatusDrift || !step.Skipped || len(step.Updated) != 0 || len(step.SkippedItems) != 1 || len(fake.calls) != 0 {
		t.Fatalf("expected safe mode to expose confirmation-only batch without applying, ok=%v step=%#v calls=%#v", ok, step, fake.calls)
	}
	if !strings.Contains(step.Reason, "can be applied after confirmation") {
		t.Fatalf("expected safe mode reason to describe confirmation boundary, got %q", step.Reason)
	}
}

func TestRunMiseBumpAutoSkipsDependencyBlockedCandidate(t *testing.T) {
	root := t.TempDir()
	codex := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	broot := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "cargo:broot",
		InstalledVersions: []string{"1.56.0"},
		CurrentVersion:    "1.57.0",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	validateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	firstPreflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "cargo:broot", "github:openai/codex"}, "\x00")
	secondPreflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"}, "\x00")
	applyKey := strings.Join([]string{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"}, "\x00")
	fake := &fakeCommandRunner{
		results: map[string]runner.Result{
			validateKey:        {Stdout: `{"cargo:broot":{"requested":"1.56.0","current":"1.56.0","bump":"1.57.0","latest":"1.57.0"},"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"0.60.1","latest":"0.60.1"}}`},
			firstPreflightKey:  {Stderr: "mise WARN tool 'cargo:broot@1.57.0': depends on 'rust' which is not in the current install set"},
			secondPreflightKey: {Stdout: "Would bump github:openai/codex"},
			applyKey:           {Stdout: "github:openai/codex 0.60.0 -> 0.60.1"},
		},
	}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{codex, broot},
	}}, false)
	if !ok || step.Status != plan.StatusHeld || len(step.Updated) != 1 || !strings.Contains(step.Updated[0], "github:openai/codex") || len(step.SkippedItems) != 1 || !strings.Contains(step.SkippedItems[0], "cargo:broot") {
		t.Fatalf("expected dependency-blocked candidate to be skipped while remaining safe candidate applies, ok=%v step=%#v", ok, step)
	}
	if fakeCommandWasCalled(fake.calls, strings.Split(strings.Join([]string{"mise", "upgrade", "--bump", "--yes", "--cd", root, "cargo:broot", "github:openai/codex"}, "\x00"), "\x00")) {
		t.Fatalf("dependency-blocked candidate must not be passed to apply command, calls=%#v", fake.calls)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(applyKey, "\x00")) {
		t.Fatalf("expected remaining safe candidate to be applied, calls=%#v", fake.calls)
	}
	if !updateStepHasCommands(step, [][]string{
		{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "cargo:broot", "github:openai/codex"},
		{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"},
		{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"},
	}) {
		t.Fatalf("expected dependency-blocked bump report to include both preflights and apply command, got %#v", step.Commands)
	}
}

func TestRunMiseBumpAutoHoldsWhenAllCandidatesAreDependencyBlocked(t *testing.T) {
	root := t.TempDir()
	broot := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "cargo:broot",
		InstalledVersions: []string{"1.56.0"},
		CurrentVersion:    "1.57.0",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	validateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	preflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "cargo:broot"}, "\x00")
	fake := &fakeCommandRunner{
		results: map[string]runner.Result{
			validateKey:  {Stdout: `{"cargo:broot":{"requested":"1.56.0","current":"1.56.0","bump":"1.57.0","latest":"1.57.0"}}`},
			preflightKey: {Stderr: "mise WARN tool 'cargo:broot@1.57.0': depends on 'rust' which is not in the current install set"},
		},
	}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{broot},
	}}, false)
	if !ok || step.Status != plan.StatusHeld || !step.Skipped || !strings.Contains(step.Reason, "dependency-blocked") {
		t.Fatalf("expected dependency-blocked only candidates to hold, ok=%v step=%#v", ok, step)
	}
	if !updateStepHasCommands(step, [][]string{
		{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "cargo:broot"},
	}) {
		t.Fatalf("expected only the executed preflight command in report, got %#v", step.Commands)
	}
}

func TestSanitizedNPMUserConfigForMiseBumpKeepsRegistryAndDropsReleaseAge(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	configDir := filepath.Join(home, ".config", "npm")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".npmrc"), []byte("registry=https://registry.npmjs.org/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "npmrc"), []byte("@webkaz-labs:registry=https://npm.pkg.github.com\n//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}\nmin-release-age=3\nminimum_release_age=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	content := sanitizedNPMUserConfigContentForMiseBump()
	for _, want := range []string{
		"registry=https://registry.npmjs.org/",
		"@webkaz-labs:registry=https://npm.pkg.github.com",
		"//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected sanitized npm config to keep %q, got:\n%s", want, content)
		}
	}
	if strings.Contains(content, "min-release-age") || strings.Contains(content, "minimum_release_age") {
		t.Fatalf("expected sanitized npm config to drop release-age settings, got:\n%s", content)
	}
}

func TestNPMUserConfigCandidatePathsPreferExplicitConfig(t *testing.T) {
	home := t.TempDir()
	explicit := filepath.Join(home, "custom-npmrc")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("NPM_CONFIG_USERCONFIG", explicit)

	got := npmUserConfigCandidatePaths()
	if len(got) != 1 || got[0] != explicit {
		t.Fatalf("expected explicit npm userconfig to be the only candidate, got %#v", got)
	}
}

func TestNPMUserConfigCandidatePathsUseHomeAndXDGConfig(t *testing.T) {
	home := t.TempDir()
	configHome := filepath.Join(home, ".config")
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("NPM_CONFIG_USERCONFIG", "")
	t.Setenv("npm_config_userconfig", "")

	want := []string{
		filepath.Join(home, ".npmrc"),
		filepath.Join(configHome, "npm", "npmrc"),
	}
	got := npmUserConfigCandidatePaths()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected npm config candidates %#v, got %#v", want, got)
	}
}

func TestRunMiseBumpAutoWrapsNPMBackendWithSanitizedUserConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	configDir := filepath.Join(home, ".config", "npm")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "npmrc"), []byte("registry=https://registry.npmjs.org/\nmin-release-age=3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	finding := safetyFinding{
		Provider:          "mise",
		Kind:              "npm",
		Name:              "npm:agent-browser",
		InstalledVersions: []string{"0.27.0"},
		CurrentVersion:    "0.27.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	validateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		validateKey: {Stdout: `{"npm:agent-browser":{"requested":"0.27.0","current":"0.27.0","bump":"0.27.1","latest":"0.27.1"}}`},
	}}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{finding},
	}}, false)
	if !ok || step.Status != plan.StatusOK || len(step.Updated) != 1 {
		t.Fatalf("expected npm bump to apply with sanitized npm config, ok=%v step=%#v", ok, step)
	}
	envCalls := [][]string{}
	for _, call := range fake.calls {
		if len(call) > 2 && call[0] == "env" && npmUserConfigAssignmentFromCommand(call) != "" {
			envCalls = append(envCalls, call)
		}
	}
	if len(envCalls) != 2 {
		t.Fatalf("expected preflight and apply to use sanitized npm userconfig, calls=%#v", fake.calls)
	}
	for _, call := range envCalls {
		if !containsString(call, "mise") || !containsString(call, "npm:agent-browser") {
			t.Fatalf("expected sanitized npm env call to wrap scoped mise bump, got %#v", call)
		}
		if !containsString(call, "-u") || !containsString(call, "NPM_CONFIG_MIN_RELEASE_AGE") || !containsString(call, "npm_config_min_release_age") {
			t.Fatalf("expected sanitized npm env call to unset release-age env vars, got %#v", call)
		}
		path := strings.TrimPrefix(npmUserConfigAssignmentFromCommand(call), "NPM_CONFIG_USERCONFIG=")
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected temporary npm config to be cleaned up, path=%q err=%v", path, err)
		}
	}
}

func npmUserConfigAssignmentFromCommand(command []string) string {
	for _, arg := range command {
		if strings.HasPrefix(arg, "NPM_CONFIG_USERCONFIG=") {
			return arg
		}
	}
	return ""
}

func TestRunMiseBumpAutoKeepsPartialUpdatesOnApplyError(t *testing.T) {
	root := t.TempDir()
	finding := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	validateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	preflightKey := strings.Join([]string{"mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:openai/codex"}, "\x00")
	applyKey := strings.Join([]string{"mise", "upgrade", "--bump", "--yes", "--cd", root, "github:openai/codex"}, "\x00")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		validateKey:  {Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"0.60.1","latest":"0.60.1"}}`},
		preflightKey: {Stdout: "Would bump github:openai/codex"},
		applyKey:     {Stdout: "github:openai/codex 0.60.0 -> 0.60.1", Stderr: "mise failed after partial update", Code: 1},
	}}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{finding},
	}}, false)
	if !ok || step.Status != plan.StatusError || len(step.Updated) != 1 || !strings.Contains(step.Updated[0], "github:openai/codex") {
		t.Fatalf("expected partial update evidence to survive apply error, ok=%v step=%#v", ok, step)
	}
}

func TestRunMiseBumpAutoHoldsWhenCandidateChangesBeforeApply(t *testing.T) {
	root := t.TempDir()
	finding := safetyFinding{
		Provider:          "mise",
		Kind:              "tool",
		Name:              "github:openai/codex",
		InstalledVersions: []string{"0.60.0"},
		CurrentVersion:    "0.60.1",
		Decision:          "allow",
		Source:            miseBumpSource,
	}
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00"): {
			Stdout: `{"github:openai/codex":{"requested":"0.60.0","current":"0.60.0","bump":"0.60.2","latest":"0.60.2"}}`,
		},
	}}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{finding},
	}}, false)
	if !ok || step.Status != plan.StatusHeld || !strings.Contains(step.Reason, "candidate set changed") {
		t.Fatalf("expected changed candidate set to hold auto bump, ok=%v step=%#v", ok, step)
	}
	if step.ReasonCode != updatereason.MiseBumpCandidateChangedApply || !strings.Contains(step.ReasonArgs["detail"], "github:openai/codex") {
		t.Fatalf("expected structured candidate-change reason, got %#v", step)
	}
	for _, call := range fake.calls {
		if len(call) >= 2 && call[0] == "mise" && call[1] == "upgrade" {
			t.Fatalf("changed candidate set must not execute upgrade, calls=%#v", fake.calls)
		}
	}
}

func TestRunMiseBumpAutoAppliesPolicyAllowedNativeAgeHold(t *testing.T) {
	root := t.TempDir()
	finding := safetyFinding{
		Provider:          "mise-bump",
		Kind:              "tool",
		Name:              "github:ogulcancelik/herdr",
		InstalledVersions: []string{"0.6.8"},
		CurrentVersion:    "0.6.9",
		Decision:          "allow",
		Reason:            "reviewed locally",
		Confidence:        "policy",
		Evidence:          []string{"mise outdated --json with MISE_MINIMUM_RELEASE_AGE=0d", "security-policy"},
		Source:            miseNativeReleaseAgeSource,
	}
	validateKey := strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	preflightKey := strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:ogulcancelik/herdr"}, "\x00")
	applyKey := strings.Join([]string{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "upgrade", "--bump", "--yes", "--cd", root, "github:ogulcancelik/herdr"}, "\x00")
	fake := &fakeCommandRunner{results: map[string]runner.Result{
		validateKey:  {Stdout: `{"github:ogulcancelik/herdr":{"requested":"0.6.8","current":"0.6.8","bump":"0.6.9","latest":"0.6.9"}}`},
		preflightKey: {Stdout: "Would bump github:ogulcancelik/herdr"},
		applyKey:     {Stdout: "github:ogulcancelik/herdr 0.6.8 -> 0.6.9"},
	}}
	step, ok := runMiseBumpUpdateStep(context.Background(), fake, updateOptions{root: root, security: "strict", miseBumpMode: "auto"}, []safetyGate{{
		Provider: miseBumpProvider,
		Status:   plan.StatusOK,
		Findings: []safetyFinding{finding},
	}}, false)
	if !ok || step.Status != plan.StatusOK || len(step.Updated) != 1 || !strings.Contains(step.Updated[0], "github:ogulcancelik/herdr") {
		t.Fatalf("expected policy-allowed native age hold to apply, ok=%v step=%#v calls=%#v", ok, step, fake.calls)
	}
	normalValidateKey := strings.Join([]string{"mise", "outdated", "--json", "--bump", "--cd", root}, "\x00")
	if fakeCommandWasCalled(fake.calls, strings.Split(normalValidateKey, "\x00")) {
		t.Fatalf("policy-allowed age hold must validate against age-disabled candidates, calls=%#v", fake.calls)
	}
	if !fakeCommandWasCalled(fake.calls, strings.Split(applyKey, "\x00")) {
		t.Fatalf("expected scoped age-disabled apply command, calls=%#v", fake.calls)
	}
	if !updateStepHasCommands(step, [][]string{
		{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "upgrade", "--dry-run", "--bump", "--cd", root, "github:ogulcancelik/herdr"},
		{"env", "MISE_MINIMUM_RELEASE_AGE=0d", "mise", "upgrade", "--bump", "--yes", "--cd", root, "github:ogulcancelik/herdr"},
	}) {
		t.Fatalf("expected policy-allowed bump report to include age-disabled preflight/apply commands, got %#v", step.Commands)
	}
}

func TestMiseBumpSafeModeExposesBatchAction(t *testing.T) {
	t.Setenv("UPDEV_MISE_BUMP_MODE", "safe")
	actions := updateStepDetailActions(updateStep{Name: miseBumpProvider, Status: plan.StatusDrift})
	found := false
	for _, action := range actions {
		parsedAction, _, ok := parseMiseBumpDetailAction(action.Value)
		if ok && parsedAction == "apply-batch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected safe mode batch action, got %#v", actions)
	}
}
