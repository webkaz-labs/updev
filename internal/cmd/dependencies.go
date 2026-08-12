package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/i18n"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/packageexecutor"
	"github.com/webkaz-labs/updev/internal/packagemetadata"
	"github.com/webkaz-labs/updev/internal/packageparity"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/support"
	"github.com/webkaz-labs/updev/internal/textui"
	"github.com/webkaz-labs/updev/internal/updevconfig"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

const (
	dependencyContractReportSchemaVersion = 1
	minimumMiseVersion                    = "2026.8.2"
)

type dependencyOptions struct {
	command string
	format  string
	root    string
	ledger  string
}

type packageReportOptions struct {
	format      string
	root        string
	interactive bool
}

type dependencyContractReport struct {
	SchemaVersion       int                           `json:"schema_version"`
	Status              plan.Status                   `json:"status"`
	Command             string                        `json:"command"`
	Root                string                        `json:"root"`
	Checks              []dependencyContractCheck     `json:"checks"`
	CompatibilityLedger dependencyCompatibilityLedger `json:"compatibility_ledger"`
}

type dependencyContractCheck struct {
	Tool                  string                `json:"tool"`
	Feature               string                `json:"feature"`
	Required              bool                  `json:"required"`
	Command               []string              `json:"command,omitempty"`
	Status                plan.Status           `json:"status"`
	Version               string                `json:"version,omitempty"`
	Value                 string                `json:"value,omitempty"`
	Source                string                `json:"source,omitempty"`
	Active                *bool                 `json:"active,omitempty"`
	CommandShapeSupported *bool                 `json:"command_shape_supported,omitempty"`
	Reason                string                `json:"reason,omitempty"`
	Remediation           string                `json:"remediation,omitempty"`
	RequiredField         []string              `json:"required_fields,omitempty"`
	MissingField          []string              `json:"missing_fields,omitempty"`
	TrustTargets          []homebrewTrustTarget `json:"trust_targets,omitempty"`
	TrustGroups           []homebrewTrustGroup  `json:"trust_groups,omitempty"`
}

type dependencyCompatibilityLedger struct {
	SchemaVersion int                            `json:"schema_version"`
	GeneratedAt   string                         `json:"generated_at"`
	Root          string                         `json:"root"`
	Entries       []dependencyCompatibilityEntry `json:"entries"`
}

type dependencyCompatibilityEntry struct {
	Tool         string      `json:"tool"`
	Feature      string      `json:"feature"`
	Required     bool        `json:"required"`
	Version      string      `json:"version,omitempty"`
	Status       plan.Status `json:"status"`
	Supported    bool        `json:"supported"`
	SupportLabel string      `json:"support_label"`
	Evidence     string      `json:"evidence,omitempty"`
	Remediation  string      `json:"remediation,omitempty"`
	Command      []string    `json:"command,omitempty"`
}

type dependencyProbe struct {
	Tool        string
	Required    bool
	Env         []string
	VersionArgs []string
	Feature     string
	JSONArgs    []string
	JSONFields  []string
	JSONRootObj bool
	CDRoot      bool
}

func runDoctor(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: updev doctor <dependencies|package-executors|package-parity|support> [--format text|json]")
		return usageExitCode
	}
	command := args[0]
	if command == "package-executors" {
		opts, err := parsePackageExecutorOptions(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runPackageExecutors(opts, runner.Local{})
	}
	if command == "package-parity" {
		opts, err := parsePackageParityOptions(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runPackageParity(opts, runner.Local{})
	}
	if command == "support" {
		opts, err := parseSupportOptions(args[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runSupport(opts)
	}
	if command != "dependencies" {
		fmt.Fprintf(os.Stderr, "unsupported doctor command: %s\n", command)
		return usageExitCode
	}
	opts, err := parseDependencyOptions(command, args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return usageExitCode
	}
	return runDependencyCheck(opts, runner.Local{})
}

func parsePackageExecutorOptions(args []string) (packageReportOptions, error) {
	return parsePackageReportOptions(args, true)
}

func parsePackageParityOptions(args []string) (packageReportOptions, error) {
	return parsePackageReportOptions(args, false)
}

func parsePackageReportOptions(args []string, allowInteractive bool) (packageReportOptions, error) {
	opts := packageReportOptions{format: "text", root: defaultRoot()}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--root":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--root requires a value")
			}
			opts.root = args[i+1]
			i++
		case "--interactive":
			if !allowInteractive {
				return opts, fmt.Errorf("unknown option: %s", args[i])
			}
			opts.interactive = true
		case "--plain", "--no-interactive":
			if !allowInteractive {
				return opts, fmt.Errorf("unknown option: %s", args[i])
			}
			opts.interactive = false
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if opts.interactive && opts.format != "text" {
		return opts, fmt.Errorf("--interactive requires --format text")
	}
	return opts, nil
}

func runPackageExecutors(opts packageReportOptions, commandRunner runner.Runner) int {
	brewfilePath := packageParityBrewfilePath(opts.root)
	config := loadUpdevConfig()
	metadataPath := updevconfig.PackageMetadataPath(config)
	platform := packageexecutor.Platform{OS: runtime.GOOS, Arch: runtime.GOARCH}
	capabilities := localPackageExecutorCapabilities(commandRunner, platform)
	report, err := buildPackageExecutorReport(context.Background(), opts.root, brewfilePath, metadataPath, commandRunner, platform, capabilities)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else if opts.interactive {
		if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
			fmt.Fprintln(os.Stderr, "--interactive requires a TTY; use --plain or --format json")
			return usageExitCode
		}
		if _, err := runDetailBrowser("updev package executor plan", packageexecutor.DetailRows(report, defaultLanguage()), textui.ColorEnabled()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	} else {
		packageexecutor.PrintText(os.Stdout, report, defaultLanguage(), textui.ColorEnabled())
	}
	if report.Status == plan.StatusDrift {
		return 2
	}
	return 0
}

func buildPackageExecutorReport(ctx context.Context, root string, brewfilePath string, metadataPath string, commandRunner runner.Runner, platform packageexecutor.Platform, capabilities packageexecutor.Capabilities) (packageexecutor.Report, error) {
	snapshot, err := packageparity.Read(ctx, root, brewfilePath, commandRunner)
	if err != nil {
		return packageexecutor.Report{}, err
	}
	return buildPackageExecutorReportFromSnapshot(snapshot, metadataPath, platform, capabilities)
}

func buildPackageExecutorReportFromSnapshot(snapshot packageparity.Snapshot, metadataPath string, platform packageexecutor.Platform, capabilities packageexecutor.Capabilities) (packageexecutor.Report, error) {
	desiredIDs, err := packageexecutor.DesiredIDs(snapshot)
	if err != nil {
		return packageexecutor.Report{}, err
	}
	metadata, err := packagemetadata.Load(metadataPath, desiredIDs)
	if err != nil {
		return packageexecutor.Report{}, err
	}
	return packageexecutor.Build(packageexecutor.Input{
		Snapshot:     snapshot,
		Metadata:     metadata,
		Platform:     platform,
		Capabilities: capabilities,
	})
}

func localPackageExecutorCapabilities(commandRunner runner.Runner, platform packageexecutor.Platform) packageexecutor.Capabilities {
	capabilities := packageexecutor.Capabilities{NativeProviders: map[string]bool{}}
	if platform.OS == "darwin" {
		_, err := commandRunner.LookPath("brew")
		capabilities.NativeProviders["brew"] = err == nil
	}
	return capabilities
}

func runPackageParity(opts packageReportOptions, commandRunner runner.Runner) int {
	brewfilePath := packageParityBrewfilePath(opts.root)
	report, err := buildPackageParityReport(context.Background(), opts.root, brewfilePath, commandRunner)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		packageparity.PrintText(os.Stdout, report, defaultLanguage(), textui.ColorEnabled())
	}
	if report.Status == plan.StatusDrift {
		return 2
	}
	return 0
}

func buildPackageParityReport(ctx context.Context, root string, brewfilePath string, commandRunner runner.Runner) (packageparity.Report, error) {
	snapshot, err := packageparity.Read(ctx, root, brewfilePath, commandRunner)
	if err != nil {
		return packageparity.Report{}, err
	}
	return snapshot.Report, nil
}

func packageParityBrewfilePath(root string) string {
	if shouldUseHomeBrewfile(root) {
		return updevpath.HomeOrRootBrewfile(root)
	}
	return updevpath.RootBrewfileTemplate(root)
}

func parseDependencyOptions(command string, args []string) (dependencyOptions, error) {
	opts := dependencyOptions{command: command, format: "text", root: defaultRoot()}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--format":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--format requires a value")
			}
			opts.format = args[i+1]
			i++
		case "--root":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--root requires a value")
			}
			opts.root = args[i+1]
			i++
		case "--ledger":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--ledger requires a value")
			}
			opts.ledger = args[i+1]
			i++
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			return opts, fmt.Errorf("unknown option: %s", args[i])
		}
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	return opts, nil
}

func runDependencyCheck(opts dependencyOptions, commandRunner runner.Runner) int {
	report := buildDependencyContractReport(context.Background(), opts, commandRunner)
	if opts.ledger != "" {
		if err := writeDependencyCompatibilityLedger(opts.ledger, report.CompatibilityLedger); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printDependencyContractText(os.Stdout, report, textui.ColorEnabled())
	}
	if report.Status == plan.StatusError {
		return 1
	}
	if report.Status == plan.StatusDrift {
		return 2
	}
	return 0
}

func buildDependencyContractReport(ctx context.Context, opts dependencyOptions, commandRunner runner.Runner) dependencyContractReport {
	checks := []dependencyContractCheck{}
	for _, probe := range dependencyProbes() {
		checks = append(checks, dependencyVersionCheck(ctx, commandRunner, probe))
		if len(probe.JSONArgs) > 0 {
			checks = append(checks, dependencyJSONContractCheck(ctx, commandRunner, probe, opts.root))
		}
	}
	for _, probe := range dependencyJSONProbes() {
		checks = append(checks, dependencyJSONContractCheck(ctx, commandRunner, probe, opts.root))
	}
	checks = append(checks, dependencyMiseConfigSourcesCheck(ctx, commandRunner, opts.root))
	checks = append(checks, dependencyMiseBootstrapPackagesDryRunCheck(ctx, commandRunner, opts.root))
	checks = append(checks, dependencyMiseMinimumReleaseAgeCheck(ctx, commandRunner, opts.root))
	checks = append(checks, homebrewTapTrustDependencyCheck(ctx, commandRunner, opts.root))
	checks = append(checks, homebrewShellWrapperDependencyCheck())
	checks = append(checks, brewfileWriteBoundaryDependencyCheck(opts.root))
	checks = append(checks, dependencyCodexTranslationCheck(commandRunner))
	sort.SliceStable(checks, func(i, j int) bool {
		if checks[i].Required != checks[j].Required {
			return checks[i].Required
		}
		if checks[i].Tool != checks[j].Tool {
			return checks[i].Tool < checks[j].Tool
		}
		return checks[i].Feature < checks[j].Feature
	})
	return dependencyContractReport{
		SchemaVersion:       dependencyContractReportSchemaVersion,
		Status:              dependencyContractStatus(checks),
		Command:             opts.command,
		Root:                opts.root,
		Checks:              checks,
		CompatibilityLedger: buildDependencyCompatibilityLedger(opts.root, checks, time.Now().UTC()),
	}
}

func dependencyMiseConfigSourcesCheck(ctx context.Context, commandRunner runner.Runner, root string) dependencyContractCheck {
	command := []string{"mise", "config", "ls", "--json", "--cd", root}
	check := dependencyContractCheck{
		Tool:     "mise",
		Feature:  "config-sources-json",
		Required: true,
		Command:  command,
		Status:   plan.StatusOK,
	}
	if _, err := commandRunner.LookPath("mise"); err != nil {
		check.Status = plan.StatusUnavailable
		check.Reason = "executable not found on PATH"
		check.Remediation = dependencyRemediation("mise", true)
		return check
	}
	result := runDependencyCommand(ctx, commandRunner, command[0], command[1:]...)
	if result.Err != nil {
		check.Status = plan.StatusError
		check.Reason = dependencyCommandError(result)
		check.Remediation = dependencyJSONRemediation("mise")
		return check
	}
	sources, err := mise.ConfigSourcesFromJSON([]byte(result.Stdout))
	if err != nil {
		check.Status = plan.StatusDrift
		check.Reason = "mise config source output is invalid: " + err.Error()
		check.Remediation = dependencyJSONRemediation("mise")
		return check
	}
	if len(sources) == 0 {
		check.Status = plan.StatusDrift
		check.Reason = "mise reported no active config sources"
		check.Remediation = "activate a mise config before relying on updev inventory or mutation"
		return check
	}
	environmentSources := 0
	paths := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Environment != "" {
			environmentSources++
		}
		paths = append(paths, source.Diagnostic())
	}
	check.Value = fmt.Sprintf("sources=%d, environment_sources=%d", len(sources), environmentSources)
	check.Source = strings.Join(paths, "; ")
	return check
}

func buildDependencyCompatibilityLedger(root string, checks []dependencyContractCheck, now time.Time) dependencyCompatibilityLedger {
	entries := make([]dependencyCompatibilityEntry, 0, len(checks))
	for _, check := range checks {
		supported := check.Status == plan.StatusOK || (!check.Required && check.Status == plan.StatusUnavailable)
		evidence := check.Version
		if evidence == "" {
			evidence = check.Value
		}
		if evidence == "" {
			evidence = check.Reason
		}
		entries = append(entries, dependencyCompatibilityEntry{
			Tool:         check.Tool,
			Feature:      check.Feature,
			Required:     check.Required,
			Version:      check.Version,
			Status:       check.Status,
			Supported:    supported,
			SupportLabel: dependencySupportLabel(check),
			Evidence:     evidence,
			Remediation:  check.Remediation,
			Command:      append([]string{}, check.Command...),
		})
	}
	return dependencyCompatibilityLedger{
		SchemaVersion: 1,
		GeneratedAt:   now.Format(time.RFC3339),
		Root:          root,
		Entries:       entries,
	}
}

func dependencySupportLabel(check dependencyContractCheck) string {
	switch check.Tool {
	case "brew", "brewfile", "mise":
		return support.LabelSupportedPreview
	case "codex", "osv-scanner", "gitleaks", "zizmor", "trivy", "grype":
		return support.LabelExperimental
	default:
		if check.Required {
			return support.LabelSupportedPreview
		}
		return support.LabelExperimental
	}
}

func homebrewShellWrapperDependencyCheck() dependencyContractCheck {
	active, ok := parseBoolValue(os.Getenv("UPDEV_BREW_WRAPPER"))
	check := dependencyContractCheck{
		Tool:        "brew",
		Feature:     "shell-wrapper",
		Required:    false,
		Status:      plan.StatusUnavailable,
		Active:      &active,
		Source:      "UPDEV_BREW_WRAPPER",
		Reason:      "Homebrew shell wrapper is not active in this updev process",
		Remediation: "start updev from a shell that loads the Homebrew wrapper, or keep using explicit updev add/list drift review actions",
	}
	if ok {
		check.Value = os.Getenv("UPDEV_BREW_WRAPPER")
	}
	if active {
		check.Status = plan.StatusOK
		check.Reason = "Homebrew shell wrapper is active"
		check.Remediation = ""
	}
	return check
}

func brewfileWriteBoundaryDependencyCheck(root string) dependencyContractCheck {
	mode := brewfileWriteMode(root)
	active := mode != "disabled"
	check := dependencyContractCheck{
		Tool:        "brewfile",
		Feature:     "write-boundary",
		Required:    false,
		Status:      plan.StatusUnavailable,
		Value:       mode,
		Active:      &active,
		Reason:      "Brewfile mutation boundary is disabled",
		Remediation: "set [brewfile].write_mode to template or chezmoi-template before using updev-managed Brewfile write actions",
	}
	if path := updevconfig.ConfigPath(); path != "" {
		check.Source = path
	} else {
		check.Source = "default"
	}
	if active {
		check.Status = plan.StatusOK
		check.Reason = "Brewfile mutation boundary is enabled"
		check.Remediation = ""
	}
	return check
}

func writeDependencyCompatibilityLedger(path string, ledger dependencyCompatibilityLedger) error {
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write compatibility ledger: %w", err)
	}
	return nil
}

func dependencyCodexTranslationCheck(commandRunner runner.Runner) dependencyContractCheck {
	mode := descriptionTranslationMode()
	active := mode == descriptionTranslationAuto && i18n.IsJapanese(defaultLanguage())
	check := dependencyContractCheck{
		Tool:     "codex",
		Feature:  "description-translation",
		Required: false,
		Command:  []string{"codex", "exec", "--skip-git-repo-check", "--output-last-message", "<tempfile>", "-"},
		Status:   plan.StatusOK,
		Active:   &active,
	}
	if mode == descriptionTranslationOff {
		check.Reason = "description translation disabled by configuration"
		return check
	}
	if _, err := commandRunner.LookPath("codex"); err != nil {
		check.Status = plan.StatusUnavailable
		check.Reason = "Codex CLI not found on PATH"
		check.Remediation = "install codex for description translation, use updev list --translate-now after installation, or set [ui].description_translation = \"off\""
		return check
	}
	check.Value = mode
	return check
}

func dependencyMiseMinimumReleaseAgeCheck(ctx context.Context, commandRunner runner.Runner, root string) dependencyContractCheck {
	check := dependencyContractCheck{
		Tool:     "mise",
		Feature:  "minimum-release-age",
		Required: true,
		Command:  []string{"mise", "settings", "ls", "--json-extended", "--cd", root},
		Status:   plan.StatusOK,
	}
	if _, err := commandRunner.LookPath("mise"); err != nil {
		check.Status = plan.StatusUnavailable
		check.Reason = "executable not found on PATH"
		check.Remediation = dependencyRemediation("mise", true)
		return check
	}
	evidence := mise.DetectMinimumReleaseAge(ctx, commandRunner, root)
	check.Status = evidence.Status
	check.Value = evidence.Value
	check.Source = evidence.Source
	check.Active = evidence.Active
	check.CommandShapeSupported = evidence.CommandShapeSupported
	check.Reason = evidence.Reason
	check.Remediation = evidence.Remediation
	return check
}

func dependencyProbes() []dependencyProbe {
	return []dependencyProbe{
		{Tool: "brew", Required: true, Env: []string{"HOMEBREW_NO_AUTO_UPDATE=1"}, VersionArgs: []string{"--version"}, Feature: "outdated-json-v2", JSONArgs: []string{"outdated", "--json=v2"}, JSONFields: []string{"formulae", "casks"}, JSONRootObj: true},
		{Tool: "mise", Required: true, VersionArgs: []string{"--version"}, Feature: "current-json", JSONArgs: []string{"ls", "--current", "--json"}, JSONRootObj: true},
		{Tool: "osv-scanner", VersionArgs: []string{"--version"}},
		{Tool: "gitleaks", VersionArgs: []string{"version"}},
		{Tool: "zizmor", VersionArgs: []string{"--version"}},
		{Tool: "trivy", VersionArgs: []string{"--version"}},
		{Tool: "grype", VersionArgs: []string{"version"}},
	}
}

func dependencyJSONProbes() []dependencyProbe {
	return []dependencyProbe{
		{Tool: "mise", Required: true, Feature: "bootstrap-status-json", JSONArgs: []string{"bootstrap", "status", "--json"}, JSONFields: []string{"packages", "tools"}, JSONRootObj: true, CDRoot: true},
		{Tool: "mise", Required: true, Feature: "bootstrap-plan-json", JSONArgs: []string{"bootstrap", "plan", "--json"}, JSONFields: []string{"resources", "summary"}, JSONRootObj: true, CDRoot: true},
	}
}

func dependencyVersionCheck(ctx context.Context, commandRunner runner.Runner, probe dependencyProbe) dependencyContractCheck {
	check := dependencyContractCheck{
		Tool:     probe.Tool,
		Feature:  "cli-version",
		Required: probe.Required,
		Command:  append([]string{probe.Tool}, probe.VersionArgs...),
		Status:   plan.StatusOK,
	}
	if _, err := commandRunner.LookPath(probe.Tool); err != nil {
		check.Status = plan.StatusUnavailable
		check.Reason = "executable not found on PATH"
		check.Remediation = dependencyRemediation(probe.Tool, probe.Required)
		return check
	}
	result := runDependencyCommand(ctx, commandRunner, probe.Tool, probe.VersionArgs...)
	if result.Err != nil {
		check.Status = plan.StatusError
		check.Reason = dependencyCommandError(result)
		check.Remediation = dependencyRemediation(probe.Tool, probe.Required)
		return check
	}
	check.Version = firstDependencyOutputLine(result.Stdout, result.Stderr)
	if check.Version == "" {
		check.Status = plan.StatusDrift
		check.Reason = "version command returned no output"
		check.Remediation = "verify the installed CLI still supports the expected version command"
		return check
	}
	if probe.Tool == "mise" && !miseVersionAtLeast(check.Version, minimumMiseVersion) {
		check.Status = plan.StatusDrift
		check.Reason = fmt.Sprintf("mise %s is older than the required v%s baseline", strings.Fields(check.Version)[0], minimumMiseVersion)
		check.Remediation = "upgrade mise to v" + minimumMiseVersion + " or newer"
	}
	return check
}

func miseVersionAtLeast(raw string, minimum string) bool {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return false
	}
	major, minor, patch := parseToolVersion(fields[0])
	minimumMajor, minimumMinor, minimumPatch := parseToolVersion(minimum)
	if major != minimumMajor {
		return major > minimumMajor
	}
	if minor != minimumMinor {
		return minor > minimumMinor
	}
	return patch >= minimumPatch
}

func dependencyJSONContractCheck(ctx context.Context, commandRunner runner.Runner, probe dependencyProbe, root string) dependencyContractCheck {
	check := dependencyContractCheck{
		Tool:          probe.Tool,
		Feature:       probe.Feature,
		Required:      probe.Required,
		Status:        plan.StatusOK,
		RequiredField: append([]string{}, probe.JSONFields...),
	}
	commandName := probe.Tool
	commandArgs := append([]string{}, probe.JSONArgs...)
	if probe.CDRoot && strings.TrimSpace(root) != "" {
		commandArgs = append(commandArgs, "--cd", root)
	}
	if len(probe.Env) > 0 {
		commandName = "env"
		commandArgs = append(append([]string{}, probe.Env...), probe.Tool)
		commandArgs = append(commandArgs, probe.JSONArgs...)
	}
	check.Command = append([]string{commandName}, commandArgs...)
	if _, err := commandRunner.LookPath(probe.Tool); err != nil {
		check.Status = plan.StatusUnavailable
		check.Reason = "executable not found on PATH"
		check.Remediation = dependencyRemediation(probe.Tool, probe.Required)
		return check
	}
	result := runDependencyCommand(ctx, commandRunner, commandName, commandArgs...)
	if result.Err != nil {
		check.Status = plan.StatusError
		check.Reason = dependencyCommandError(result)
		check.Remediation = dependencyJSONRemediation(probe.Tool)
		return check
	}
	var payload any
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		check.Status = plan.StatusDrift
		check.Reason = "command output is not valid JSON"
		check.Remediation = dependencyJSONRemediation(probe.Tool)
		return check
	}
	missing, wrongRoot := missingDependencyJSONFields(payload, probe.JSONFields, probe.JSONRootObj)
	if wrongRoot {
		check.Status = plan.StatusDrift
		check.MissingField = missing
		check.Reason = "JSON output root is not an object"
		check.Remediation = dependencyJSONRemediation(probe.Tool)
		return check
	}
	if len(missing) > 0 {
		check.Status = plan.StatusDrift
		check.MissingField = missing
		check.Reason = "JSON output is missing required fields"
		check.Remediation = dependencyJSONRemediation(probe.Tool)
	}
	if check.Status == plan.StatusOK && probe.Tool == "mise" && probe.Feature == "bootstrap-status-json" {
		packages, err := mise.BootstrapPackagesFromStatusJSON([]byte(result.Stdout))
		if err != nil {
			check.Status = plan.StatusDrift
			check.Reason = err.Error()
			check.Remediation = dependencyJSONRemediation("mise")
			return check
		}
		check.Value = fmt.Sprintf("packages=%d", len(packages))
	}
	return check
}

func dependencyMiseBootstrapPackagesDryRunCheck(ctx context.Context, commandRunner runner.Runner, root string) dependencyContractCheck {
	command := []string{"mise", "bootstrap", "packages", "apply", "--dry-run"}
	if strings.TrimSpace(root) != "" {
		command = append(command, "--cd", root)
	}
	check := dependencyContractCheck{
		Tool:     "mise",
		Feature:  "bootstrap-packages-dry-run",
		Required: true,
		Command:  command,
		Status:   plan.StatusOK,
		Value:    "dry-run supported",
	}
	if _, err := commandRunner.LookPath("mise"); err != nil {
		check.Status = plan.StatusUnavailable
		check.Value = ""
		check.Reason = "executable not found on PATH"
		check.Remediation = dependencyRemediation("mise", true)
		return check
	}
	result := runDependencyCommand(ctx, commandRunner, command[0], command[1:]...)
	if result.Err != nil {
		check.Status = plan.StatusError
		check.Value = ""
		check.Reason = dependencyCommandError(result)
		check.Remediation = "upgrade mise to v2026.8.2 or newer, or update updev if the bootstrap package dry-run contract changed"
	}
	return check
}

func runDependencyCommand(ctx context.Context, commandRunner runner.Runner, name string, args ...string) runner.Result {
	commandCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	result := commandRunner.Run(commandCtx, name, args...)
	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		result.Err = context.DeadlineExceeded
		result.Code = 124
	}
	return result
}

func dependencyContractStatus(checks []dependencyContractCheck) plan.Status {
	status := plan.StatusOK
	for _, check := range checks {
		if check.Status == plan.StatusOK || (!check.Required && check.Status == plan.StatusUnavailable) {
			continue
		}
		if check.Status == plan.StatusError && check.Required {
			return plan.StatusError
		}
		status = plan.StatusDrift
	}
	return status
}

func missingDependencyJSONFields(payload any, fields []string, rootObject bool) ([]string, bool) {
	object, ok := payload.(map[string]any)
	if !ok {
		return fields, rootObject
	}
	if len(fields) == 0 {
		return nil, false
	}
	missing := []string{}
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			missing = append(missing, field)
		}
	}
	return missing, false
}

func firstDependencyOutputLine(stdout string, stderr string) string {
	for _, text := range []string{stdout, stderr} {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				return truncate(line, 120)
			}
		}
	}
	return ""
}

func dependencyCommandError(result runner.Result) string {
	reason := strings.TrimSpace(result.Stderr)
	if reason == "" {
		reason = strings.TrimSpace(result.Stdout)
	}
	if reason == "" && errors.Is(result.Err, context.DeadlineExceeded) {
		return "command timed out"
	}
	if reason == "" && result.Err != nil {
		reason = result.Err.Error()
	}
	if reason == "" {
		reason = fmt.Sprintf("command exited with code %d", result.Code)
	}
	return truncate(strings.Join(strings.Fields(reason), " "), 240)
}

func dependencyRemediation(tool string, required bool) string {
	if required {
		return "install, repair, or expose " + tool + " before relying on updev daily checks"
	}
	return "install or upgrade " + tool + " to enable the optional scanner integration"
}

func dependencyJSONRemediation(tool string) string {
	return "repair " + tool + ", pin a compatible version, or update updev parser if the JSON contract changed"
}

func printDependencyContractText(w io.Writer, report dependencyContractReport, color bool) {
	title := "updev doctor dependencies"
	if report.Command == "check" {
		title = "updev check --dependencies"
	}
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading(title, color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("root:", color), report.Root)
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("checks:", color), textui.StyleCount(fmt.Sprint(len(report.Checks)), color))
	rows := make([][]string, 0, len(report.Checks))
	for _, check := range report.Checks {
		scope := "optional"
		if check.Required {
			scope = "required"
		}
		detail := check.Version
		if detail == "" {
			if check.Value != "" {
				detail = check.Value
				if check.Source != "" {
					detail += " from " + check.Source
				}
			}
		}
		if detail == "" {
			detail = check.Reason
		}
		if check.CommandShapeSupported != nil {
			shape := "latest flag: unsupported"
			if *check.CommandShapeSupported {
				shape = "latest flag: supported"
			}
			if detail == "" {
				detail = shape
			} else {
				detail += "; " + shape
			}
		}
		if len(check.MissingField) > 0 {
			detail = "missing fields: " + strings.Join(check.MissingField, ",")
		}
		rows = append(rows, []string{
			textui.StyleName(check.Tool, color),
			check.Feature,
			dependencySupportLabel(check),
			scope,
			textui.StyleStatus(string(check.Status), color),
			detail,
		})
	}
	fmt.Fprintln(w, "\nchecks")
	textui.PrintTable(w, []textui.Column{
		{Header: "tool", Min: 8, Max: 14},
		{Header: "feature", Min: 12, Max: 20},
		{Header: "support_label", Min: 14, Max: 18},
		{Header: "scope", Min: 8, Max: 8},
		{Header: "status", Min: 10, Max: 12},
		{Header: "detail", Min: 20, Max: 80},
	}, rows, color)
	printDependencyTrustGroups(w, report, color)
	if report.Status == plan.StatusOK {
		return
	}
	fmt.Fprintln(w, "\nnext")
	for _, check := range report.Checks {
		if check.Status == plan.StatusOK || (!check.Required && check.Status == plan.StatusUnavailable) {
			continue
		}
		fmt.Fprintf(w, "  %s: %s\n", check.Tool, check.Remediation)
	}
}

func printDependencyTrustGroups(w io.Writer, report dependencyContractReport, color bool) {
	var groups []homebrewTrustGroup
	for _, check := range report.Checks {
		if check.Tool == "brew" && check.Feature == "tap-trust" {
			groups = check.TrustGroups
			break
		}
	}
	if len(groups) == 0 {
		return
	}
	rows := [][]string{}
	for _, group := range groups {
		if group.Trusted {
			continue
		}
		rows = append(rows, []string{
			textui.StyleName(group.Tap, color),
			textui.StyleStatus("drift", color),
			fmt.Sprintf("%d/%d", group.UntrustedCount, group.TargetCount),
			joinCommand(group.TrustCommandArgv),
		})
	}
	if len(rows) == 0 {
		return
	}
	fmt.Fprintln(w, "\nhomebrew trust")
	textui.PrintTable(w, []textui.Column{
		{Header: "tap", Min: 16, Max: 32},
		{Header: "status", Min: 8, Max: 10},
		{Header: "untrusted", Min: 10, Max: 10},
		{Header: "command", Min: 24, Max: 80},
	}, rows, color)
}
