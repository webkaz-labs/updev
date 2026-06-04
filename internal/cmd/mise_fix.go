package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/textui"
)

const miseManifestFixReportSchemaVersion = 1

type miseManifestFixOptions struct {
	format string
	root   string
	apply  bool
}

type miseManifestFixReport struct {
	SchemaVersion         int                           `json:"schema_version"`
	Status                plan.Status                   `json:"status"`
	Root                  string                        `json:"root"`
	DryRun                bool                          `json:"dry_run"`
	MiseMinimumReleaseAge miseMinimumReleaseAgeEvidence `json:"mise_minimum_release_age"`
	Actions               []miseManifestFixAction       `json:"actions"`
	Error                 string                        `json:"error,omitempty"`
}

type miseManifestFixAction struct {
	Tool                  string      `json:"tool"`
	Path                  string      `json:"path"`
	Line                  int         `json:"line"`
	Current               string      `json:"current,omitempty"`
	Resolved              string      `json:"resolved,omitempty"`
	Command               []string    `json:"command,omitempty"`
	AgePolicyActive       bool        `json:"age_policy_active"`
	AgePolicyValue        string      `json:"age_policy_value,omitempty"`
	AgePolicySource       string      `json:"age_policy_source,omitempty"`
	CommandShapeSupported bool        `json:"command_shape_supported"`
	Status                plan.Status `json:"status"`
	Reason                string      `json:"reason,omitempty"`
}

func runFix(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: updev fix mise [--dry-run|--apply] [--format text|json]")
		return usageExitCode
	}
	command := args[0]
	if command != "mise" {
		fmt.Fprintf(os.Stderr, "unsupported fix command: %s\n", command)
		return usageExitCode
	}
	opts, err := parseMiseManifestFixOptions(args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return usageExitCode
	}
	report := buildMiseManifestFixReport(context.Background(), opts, runner.Local{})
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printMiseManifestFixText(os.Stdout, report, textui.ColorEnabled())
	}
	return updateExitCode(report.Status)
}

func parseMiseManifestFixOptions(args []string) (miseManifestFixOptions, error) {
	opts := miseManifestFixOptions{format: "text", root: defaultRoot()}
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
		case "--dry-run":
			opts.apply = false
		case "--apply":
			opts.apply = true
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

func buildMiseManifestFixReport(ctx context.Context, opts miseManifestFixOptions, commandRunner commandRunner) miseManifestFixReport {
	report := miseManifestFixReport{
		SchemaVersion: miseManifestFixReportSchemaVersion,
		Status:        plan.StatusOK,
		Root:          opts.root,
		DryRun:        !opts.apply,
		Actions:       []miseManifestFixAction{},
	}
	report.MiseMinimumReleaseAge = detectMiseMinimumReleaseAge(ctx, commandRunner, opts.root)
	issues, err := mise.ManifestIssues(opts.root)
	if err != nil {
		report.Status = plan.StatusError
		report.Error = err.Error()
		return report
	}
	latestIssues := miseLatestIssues(issues)
	if len(latestIssues) == 0 {
		return report
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	replacements := map[string]map[int]string{}
	for _, issue := range latestIssues {
		action := miseManifestFixAction{
			Tool:                  issue.Tool,
			Path:                  issue.Path,
			Line:                  issue.Line,
			Current:               issue.Version,
			Command:               []string{"mise", "latest", issue.Tool},
			AgePolicyActive:       report.MiseMinimumReleaseAge.Active,
			AgePolicyValue:        report.MiseMinimumReleaseAge.Value,
			AgePolicySource:       report.MiseMinimumReleaseAge.Source,
			CommandShapeSupported: report.MiseMinimumReleaseAge.CommandShapeSupported,
			Status:                plan.StatusUnavailable,
		}
		resolved, reason := resolveMiseLatestVersion(ctx, commandRunner, issue.Tool)
		if reason != "" {
			action.Reason = reason
			report.Actions = append(report.Actions, action)
			continue
		}
		action.Resolved = resolved
		action.Status = plan.StatusDrift
		if replacements[issue.Path] == nil {
			replacements[issue.Path] = map[int]string{}
		}
		replacements[issue.Path][issue.Line] = resolved
		report.Actions = append(report.Actions, action)
	}
	if !opts.apply {
		report.Status = plan.StatusDrift
		return report
	}
	if len(replacements) == 0 {
		report.Status = plan.StatusDrift
		return report
	}
	changedPaths, err := applyMiseManifestFixes(replacements)
	if err != nil {
		report.Status = plan.StatusError
		report.Error = err.Error()
		return report
	}
	for index := range report.Actions {
		if report.Actions[index].Status == plan.StatusDrift {
			if changedPaths[report.Actions[index].Path] {
				report.Actions[index].Status = plan.StatusOK
				report.Actions[index].Reason = "updated mise manifest"
			} else {
				report.Actions[index].Status = plan.StatusOK
				report.Actions[index].Reason = "manifest already matched resolved version"
			}
		}
	}
	if hasUnavailableMiseFixAction(report.Actions) {
		report.Status = plan.StatusDrift
		return report
	}
	report.Status = plan.StatusOK
	return report
}

func miseLatestIssues(issues []mise.ManifestIssue) []mise.ManifestIssue {
	out := []mise.ManifestIssue{}
	for _, issue := range issues {
		if strings.EqualFold(strings.TrimSpace(issue.Version), "latest") {
			out = append(out, issue)
		}
	}
	return out
}

func resolveMiseLatestVersion(ctx context.Context, commandRunner commandRunner, tool string) (string, string) {
	result := commandRunner.Run(ctx, "mise", "latest", tool)
	if result.Code != 0 || result.Err != nil {
		return "", "mise latest failed: " + firstNonEmpty(result.Stderr, result.Stdout, fmt.Sprint(result.Err))
	}
	resolved := strings.TrimSpace(strings.Split(strings.TrimSpace(result.Stdout), "\n")[0])
	if resolved == "" {
		return "", "mise latest returned an empty version"
	}
	if reason := miseResolvedVersionPinReason(tool, resolved); reason != "" {
		return "", reason
	}
	return resolved, ""
}

func miseResolvedVersionPinReason(tool string, version string) string {
	normalized := strings.ToLower(strings.TrimSpace(version))
	if normalized == "" || normalized == "latest" {
		return "resolved version is not an exact version"
	}
	if normalized == "lts" {
		return "resolved version is lts; choose an exact version manually"
	}
	return ""
}

func applyMiseManifestFixes(replacementsByPath map[string]map[int]string) (map[string]bool, error) {
	changedPaths := map[string]bool{}
	paths := make([]string, 0, len(replacementsByPath))
	for path := range replacementsByPath {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		replacements := replacementsByPath[path]
		changed, err := applyMiseManifestFixesForPath(path, replacements)
		if err != nil {
			return changedPaths, err
		}
		changedPaths[path] = changed
	}
	return changedPaths, nil
}

func applyMiseManifestFixesForPath(path string, replacements map[int]string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	lines := strings.SplitAfter(string(data), "\n")
	changed := false
	for lineNumber, replacement := range replacements {
		if lineNumber <= 0 || lineNumber > len(lines) {
			continue
		}
		index := lineNumber - 1
		updated, ok := rewriteMiseLatestLine(lines[index], replacement)
		if ok && updated != lines[index] {
			lines[index] = updated
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "")), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, nil
}

func rewriteMiseLatestLine(line string, replacement string) (string, bool) {
	body, comment, newline := splitLineComment(line)
	quoted := strconv.Quote(replacement)
	for _, token := range []string{`version = "latest"`, `version="latest"`, `version = 'latest'`, `version='latest'`} {
		if strings.Contains(body, token) {
			return strings.Replace(body, token, strings.Replace(token, "latest", replacement, 1), 1) + comment + newline, true
		}
	}
	index := strings.Index(body, "=")
	if index < 0 {
		return line, false
	}
	if !strings.EqualFold(strings.Trim(strings.TrimSpace(body[index+1:]), `"'`), "latest") {
		return line, false
	}
	return body[:index+1] + " " + quoted + formattedLineComment(comment) + newline, true
}

func formattedLineComment(comment string) string {
	if comment == "" {
		return ""
	}
	if strings.HasPrefix(comment, " ") || strings.HasPrefix(comment, "\t") {
		return comment
	}
	return " " + comment
}

func splitLineComment(line string) (string, string, string) {
	newline := ""
	if strings.HasSuffix(line, "\n") {
		newline = "\n"
		line = strings.TrimSuffix(line, "\n")
	}
	inSingle := false
	inDouble := false
	for index, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:index], line[index:], newline
			}
		}
	}
	return line, "", newline
}

func hasUnavailableMiseFixAction(actions []miseManifestFixAction) bool {
	for _, action := range actions {
		if action.Status == plan.StatusUnavailable {
			return true
		}
	}
	return false
}

func printMiseManifestFixText(w io.Writer, report miseManifestFixReport, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.StyleHeading("updev fix mise", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("root:", color), report.Root)
	if report.DryRun {
		fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("mode:", color), textui.StyleRequested("dry-run", color))
	}
	fmt.Fprintf(w, "%s %s\n", textui.StyleLabel("mise minimum_release_age:", color), miseMinimumReleaseAgeText(report.MiseMinimumReleaseAge))
	if report.Error != "" {
		fmt.Fprintf(w, "%s %s\n", textui.StyleError("error:", color), report.Error)
	}
	rows := make([][]string, 0, len(report.Actions))
	for _, action := range report.Actions {
		rows = append(rows, []string{
			textui.StyleName(action.Tool, color),
			textui.StyleStatus(string(action.Status), color),
			textui.StyleVersion(action.Current+" -> "+action.Resolved, color),
			action.Path + ":" + strconv.Itoa(action.Line),
			action.Reason,
		})
	}
	if len(rows) == 0 {
		fmt.Fprintf(w, "  %s\n", textui.StyleDim("no resolvable mise latest entries", color))
		return
	}
	textui.PrintTable(w, []textui.Column{
		{Header: "tool", Min: 10, Max: 28},
		{Header: "status", Min: 8, Max: 12},
		{Header: "version", Min: 12, Max: 32},
		{Header: "source", Min: 16, Max: 56},
		{Header: "reason", Min: 0, Max: 72},
	}, rows, color)
}

func miseMinimumReleaseAgeText(evidence miseMinimumReleaseAgeEvidence) string {
	shape := "latest flag unsupported"
	if evidence.CommandShapeSupported {
		shape = "latest flag supported"
	}
	if evidence.Active {
		if evidence.Source != "" {
			return "active " + evidence.Value + " from " + evidence.Source + "; " + shape
		}
		return "active " + evidence.Value + "; " + shape
	}
	if evidence.Reason != "" {
		return "inactive; " + evidence.Reason + "; " + shape
	}
	return "inactive; " + shape
}
