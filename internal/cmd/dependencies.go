package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/textui"
)

const dependencyContractReportSchemaVersion = 1

type dependencyOptions struct {
	command string
	format  string
	root    string
}

type dependencyContractReport struct {
	SchemaVersion int                       `json:"schema_version"`
	Status        plan.Status               `json:"status"`
	Command       string                    `json:"command"`
	Root          string                    `json:"root"`
	Checks        []dependencyContractCheck `json:"checks"`
}

type dependencyContractCheck struct {
	Tool                  string      `json:"tool"`
	Feature               string      `json:"feature"`
	Required              bool        `json:"required"`
	Command               []string    `json:"command,omitempty"`
	Status                plan.Status `json:"status"`
	Version               string      `json:"version,omitempty"`
	Value                 string      `json:"value,omitempty"`
	Source                string      `json:"source,omitempty"`
	Active                *bool       `json:"active,omitempty"`
	CommandShapeSupported *bool       `json:"command_shape_supported,omitempty"`
	Reason                string      `json:"reason,omitempty"`
	Remediation           string      `json:"remediation,omitempty"`
	RequiredField         []string    `json:"required_fields,omitempty"`
	MissingField          []string    `json:"missing_fields,omitempty"`
}

type dependencyProbe struct {
	Tool        string
	Required    bool
	VersionArgs []string
	Feature     string
	JSONArgs    []string
	JSONFields  []string
	JSONRootObj bool
}

func runDoctor(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: updev doctor dependencies [--format text|json]")
		return usageExitCode
	}
	command := args[0]
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
			checks = append(checks, dependencyJSONContractCheck(ctx, commandRunner, probe))
		}
	}
	checks = append(checks, dependencyMiseMinimumReleaseAgeCheck(ctx, commandRunner, opts.root))
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
		SchemaVersion: dependencyContractReportSchemaVersion,
		Status:        dependencyContractStatus(checks),
		Command:       opts.command,
		Root:          opts.root,
		Checks:        checks,
	}
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
	evidence := detectMiseMinimumReleaseAge(ctx, commandRunner, root)
	check.Status = evidence.Status
	check.Value = evidence.Value
	check.Source = evidence.Source
	check.Active = &evidence.Active
	check.CommandShapeSupported = &evidence.CommandShapeSupported
	check.Reason = evidence.Reason
	check.Remediation = evidence.Remediation
	return check
}

func dependencyProbes() []dependencyProbe {
	return []dependencyProbe{
		{Tool: "brew", Required: true, VersionArgs: []string{"--version"}, Feature: "outdated-json-v2", JSONArgs: []string{"outdated", "--json=v2"}, JSONFields: []string{"formulae", "casks"}, JSONRootObj: true},
		{Tool: "mise", Required: true, VersionArgs: []string{"--version"}, Feature: "current-json", JSONArgs: []string{"ls", "--current", "--json"}, JSONRootObj: true},
		{Tool: "osv-scanner", VersionArgs: []string{"--version"}},
		{Tool: "gitleaks", VersionArgs: []string{"version"}},
		{Tool: "zizmor", VersionArgs: []string{"--version"}},
		{Tool: "trivy", VersionArgs: []string{"--version"}},
		{Tool: "grype", VersionArgs: []string{"version"}},
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
	}
	return check
}

func dependencyJSONContractCheck(ctx context.Context, commandRunner runner.Runner, probe dependencyProbe) dependencyContractCheck {
	check := dependencyContractCheck{
		Tool:          probe.Tool,
		Feature:       probe.Feature,
		Required:      probe.Required,
		Command:       append([]string{probe.Tool}, probe.JSONArgs...),
		Status:        plan.StatusOK,
		RequiredField: append([]string{}, probe.JSONFields...),
	}
	if _, err := commandRunner.LookPath(probe.Tool); err != nil {
		check.Status = plan.StatusUnavailable
		check.Reason = "executable not found on PATH"
		check.Remediation = dependencyRemediation(probe.Tool, probe.Required)
		return check
	}
	result := runDependencyCommand(ctx, commandRunner, probe.Tool, probe.JSONArgs...)
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
			scope,
			textui.StyleStatus(string(check.Status), color),
			detail,
		})
	}
	fmt.Fprintln(w, "\nchecks")
	textui.PrintTable(w, []textui.Column{
		{Header: "tool", Min: 8, Max: 14},
		{Header: "feature", Min: 12, Max: 20},
		{Header: "scope", Min: 8, Max: 8},
		{Header: "status", Min: 10, Max: 12},
		{Header: "detail", Min: 20, Max: 80},
	}, rows, color)
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
