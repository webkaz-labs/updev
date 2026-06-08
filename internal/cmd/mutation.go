package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/brewfile"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/snapshot"
)

const mutationReportSchemaVersion = 1
const rollbackReportSchemaVersion = 1

type mutationOptions struct {
	action   string
	format   string
	root     string
	provider string
	kind     string
	name     string
	category string
	version  string
}

type mutationReport struct {
	SchemaVersion   int              `json:"schema_version"`
	Status          plan.Status      `json:"status"`
	Action          string           `json:"action"`
	Root            string           `json:"root"`
	Provider        string           `json:"provider,omitempty"`
	Kind            string           `json:"kind,omitempty"`
	Name            string           `json:"name,omitempty"`
	Changed         bool             `json:"changed"`
	ChangedFiles    []string         `json:"changed_files,omitempty"`
	Diff            string           `json:"diff,omitempty"`
	Validation      validationResult `json:"validation"`
	Snapshot        *snapshotRef     `json:"snapshot,omitempty"`
	RollbackCommand string           `json:"rollback_command,omitempty"`
	Reason          string           `json:"reason,omitempty"`
	Candidates      []mutationTarget `json:"candidates,omitempty"`
}

type mutationTarget struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Reason   string `json:"reason,omitempty"`
}

type validationResult struct {
	Status plan.Status `json:"status"`
	Reason string      `json:"reason,omitempty"`
}

type snapshotRef struct {
	Token string `json:"token"`
	Files int    `json:"files"`
}

type rollbackOptions struct {
	format string
	root   string
	token  string
}

type rollbackReport struct {
	SchemaVersion int         `json:"schema_version"`
	Status        plan.Status `json:"status"`
	Root          string      `json:"root"`
	Token         string      `json:"token,omitempty"`
	RestoredFiles []string    `json:"restored_files,omitempty"`
	Error         string      `json:"error,omitempty"`
}

func parseMutationOptions(action string, args []string) (mutationOptions, error) {
	opts := mutationOptions{action: action, format: "text", root: defaultRoot()}
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
		case "--provider":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--provider requires a value")
			}
			opts.provider = args[i+1]
			i++
		case "--kind":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--kind requires a value")
			}
			opts.kind = args[i+1]
			i++
		case "--category":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--category requires a value")
			}
			opts.category = args[i+1]
			i++
		case "--version":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--version requires a value")
			}
			opts.version = args[i+1]
			i++
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			if strings.HasPrefix(args[i], "-") {
				return opts, fmt.Errorf("unknown option: %s", args[i])
			}
			if opts.name != "" {
				return opts, fmt.Errorf("only one name is supported")
			}
			opts.name = args[i]
		}
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if opts.action != "edit" && opts.name == "" {
		return opts, fmt.Errorf("%s requires a name", action)
	}
	return opts, nil
}

func runMutation(opts mutationOptions) int {
	progress := startupProgress{}
	if opts.format == "text" {
		progress = newStartupProgress(os.Stdin, os.Stderr, opts.format, mutationProgressMessage(defaultLanguage(), opts.action))
	}
	progress.Start()
	report := buildMutationReport(context.Background(), opts)
	progress.Done()
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printMutationText(os.Stdout, report)
	}
	return updateExitCode(report.Status)
}

func buildMutationReport(ctx context.Context, opts mutationOptions) mutationReport {
	report := mutationReport{
		SchemaVersion: mutationReportSchemaVersion,
		Status:        plan.StatusOK,
		Action:        opts.action,
		Root:          opts.root,
		Name:          opts.name,
	}
	target, candidates, err := resolveMutationTarget(opts)
	if err != nil {
		report.Status = plan.StatusHeld
		report.Reason = err.Error()
		report.Candidates = candidates
		return report
	}
	report.Provider = target.Provider
	report.Kind = target.Kind
	if target.Provider == "brew" {
		if err := ensureBrewfileWriteAllowed(opts.root); err != nil {
			report.Status = plan.StatusHeld
			report.Reason = err.Error()
			return report
		}
	}
	before := readManifestContents(manifestPaths(opts.root))
	snap, err := snapshot.Create(opts.root, manifestPaths(opts.root))
	if err != nil {
		report.Status = plan.StatusError
		report.Reason = err.Error()
		return report
	}
	report.Snapshot = &snapshotRef{Token: snap.Token, Files: len(snap.Files)}
	report.RollbackCommand = "updev rollback --token " + snap.Token
	changed, err := applyMutationTarget(opts, target)
	if err != nil {
		report.Status = plan.StatusError
		report.Reason = err.Error()
		return report
	}
	report.Changed = changed
	after := readManifestContents(manifestPaths(opts.root))
	report.ChangedFiles, report.Diff = diffManifestContents(before, after)
	report.Validation = validateAfterMutation(ctx, opts.root)
	report.Status = statusAfterValidation(report.Status, report.Validation)
	if !changed {
		report.Reason = "no manifest change was needed"
	}
	return report
}

func ensureBrewfileWriteAllowed(root string) error {
	mode := brewfileWriteMode(root)
	if mode == "disabled" {
		return fmt.Errorf("Brewfile writes are disabled; set [brewfile].write_mode to direct, template, or chezmoi-template before mutating Homebrew desired state")
	}
	return nil
}

func brewfileWriteMode(root string) string {
	if configured := loadUpdevConfig().Brewfile.WriteMode; configured != nil {
		return strings.ToLower(strings.TrimSpace(*configured))
	}
	return "disabled"
}

func resolveMutationTarget(opts mutationOptions) (mutationTarget, []mutationTarget, error) {
	name := strings.TrimSpace(opts.name)
	provider := strings.ToLower(strings.TrimSpace(opts.provider))
	kind := strings.ToLower(strings.TrimSpace(opts.kind))
	if provider == "brew" || isBrewfileKind(kind) {
		if kind == "" {
			return mutationTarget{}, []mutationTarget{
				{Provider: "brew", Kind: "brew", Name: name, Reason: "Homebrew formula"},
				{Provider: "brew", Kind: "cask", Name: name, Reason: "Homebrew cask"},
				{Provider: "brew", Kind: "tap", Name: name, Reason: "Homebrew tap"},
				{Provider: "brew", Kind: "vscode", Name: name, Reason: "VS Code extension"},
			}, fmt.Errorf("brew add/remove requires --kind brew|cask|tap|vscode")
		}
		if opts.action == "add" && opts.category == "" {
			return mutationTarget{}, nil, fmt.Errorf("brew add requires --category work|personal")
		}
		return mutationTarget{Provider: "brew", Kind: kind, Name: name}, nil, nil
	}
	if provider == "mise" || kind == "tool" || strings.Contains(name, ":") {
		return mutationTarget{Provider: "mise", Kind: "tool", Name: name}, nil, nil
	}
	if provider == "" && kind == "" {
		return mutationTarget{}, []mutationTarget{
			{Provider: "mise", Kind: "tool", Name: name, Reason: "CLI tool; use --provider mise or --kind tool"},
			{Provider: "brew", Kind: "brew", Name: name, Reason: "Homebrew formula; use --provider brew --kind brew --category work|personal"},
			{Provider: "brew", Kind: "cask", Name: name, Reason: "Homebrew cask; use --provider brew --kind cask --category work|personal"},
		}, fmt.Errorf("ambiguous package/tool name; choose --provider and --kind")
	}
	return mutationTarget{}, nil, fmt.Errorf("unsupported provider/kind combination")
}

func applyMutationTarget(opts mutationOptions, target mutationTarget) (bool, error) {
	switch target.Provider {
	case "brew":
		if opts.action == "add" {
			return brewfile.AddEntry(opts.root, target.Kind, target.Name, opts.category)
		}
		return brewfile.RemoveEntry(opts.root, target.Kind, target.Name)
	case "mise":
		if opts.action == "add" {
			return mise.AddTool(opts.root, target.Name, opts.version)
		}
		return mise.RemoveTool(opts.root, target.Name)
	default:
		return false, fmt.Errorf("unsupported provider: %s", target.Provider)
	}
}

func isBrewfileKind(kind string) bool {
	switch kind {
	case "brew", "cask", "tap", "vscode":
		return true
	default:
		return false
	}
}

func validateAfterMutation(ctx context.Context, root string) validationResult {
	report := collectInventory(ctx, root, runner.Local{})
	if report.Status == plan.StatusError {
		return validationResult{Status: plan.StatusError, Reason: "inventory check failed after mutation"}
	}
	return validationResult{Status: report.Status, Reason: "inventory check completed after mutation"}
}

func statusAfterValidation(status plan.Status, validation validationResult) plan.Status {
	if status != plan.StatusOK {
		return status
	}
	if validation.Status == plan.StatusError {
		return plan.StatusError
	}
	return status
}

func manifestPaths(root string) []string {
	return []string{
		brewfile.SourcePath(root),
		mise.ConfigPath(root),
	}
}

func readManifestContents(paths []string) map[string]string {
	out := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		out[path] = string(data)
	}
	return out
}

func diffManifestContents(before map[string]string, after map[string]string) ([]string, string) {
	changed := []string{}
	var diff strings.Builder
	for _, path := range sortedManifestPaths(before, after) {
		afterText := after[path]
		beforeText := before[path]
		if beforeText == afterText {
			continue
		}
		changed = append(changed, path)
		diff.WriteString(simpleUnifiedDiff(path, beforeText, afterText))
	}
	return changed, diff.String()
}

func sortedManifestPaths(before map[string]string, after map[string]string) []string {
	seen := map[string]bool{}
	paths := []string{}
	for path := range before {
		seen[path] = true
		paths = append(paths, path)
	}
	for path := range after {
		if !seen[path] {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func simpleUnifiedDiff(path string, before string, after string) string {
	var out strings.Builder
	out.WriteString("--- " + path + "\n")
	out.WriteString("+++ " + path + "\n")
	for _, op := range lineDiff(splitDiffLines(before), splitDiffLines(after)) {
		out.WriteByte(op.prefix)
		out.WriteString(op.line)
		out.WriteByte('\n')
	}
	return out.String()
}

type diffLine struct {
	prefix byte
	line   string
}

func splitDiffLines(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func lineDiff(before []string, after []string) []diffLine {
	lcs := make([][]int, len(before)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(after)+1)
	}
	for i := len(before) - 1; i >= 0; i-- {
		for j := len(after) - 1; j >= 0; j-- {
			if before[i] == after[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	out := []diffLine{}
	i, j := 0, 0
	for i < len(before) && j < len(after) {
		switch {
		case before[i] == after[j]:
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, diffLine{prefix: '-', line: before[i]})
			i++
		default:
			out = append(out, diffLine{prefix: '+', line: after[j]})
			j++
		}
	}
	for i < len(before) {
		out = append(out, diffLine{prefix: '-', line: before[i]})
		i++
	}
	for j < len(after) {
		out = append(out, diffLine{prefix: '+', line: after[j]})
		j++
	}
	return out
}

func printMutationText(w io.Writer, report mutationReport) {
	fmt.Fprintf(w, "updev %s [%s]\n", report.Action, report.Status)
	fmt.Fprintf(w, "root: %s\n", report.Root)
	if report.Provider != "" {
		fmt.Fprintf(w, "target: %s/%s %s\n", report.Provider, report.Kind, report.Name)
	}
	if report.Reason != "" {
		fmt.Fprintf(w, "reason: %s\n", report.Reason)
	}
	if len(report.Candidates) > 0 {
		fmt.Fprintln(w, "candidates:")
		for _, candidate := range report.Candidates {
			fmt.Fprintf(w, "  %s/%s %s - %s\n", candidate.Provider, candidate.Kind, candidate.Name, candidate.Reason)
		}
	}
	if report.Snapshot != nil {
		fmt.Fprintf(w, "snapshot: %s\n", report.Snapshot.Token)
	}
	fmt.Fprintf(w, "changed: %t\n", report.Changed)
	if len(report.ChangedFiles) > 0 {
		fmt.Fprintln(w, "changed files:")
		for _, file := range report.ChangedFiles {
			fmt.Fprintf(w, "  %s\n", file)
		}
	}
	if report.Diff != "" {
		fmt.Fprintln(w, "\ndiff")
		fmt.Fprint(w, report.Diff)
	}
	fmt.Fprintf(w, "\nvalidation: %s", report.Validation.Status)
	if report.Validation.Reason != "" {
		fmt.Fprintf(w, " - %s", report.Validation.Reason)
	}
	fmt.Fprintln(w)
	if report.RollbackCommand != "" {
		fmt.Fprintf(w, "rollback: %s\n", report.RollbackCommand)
	}
	printMutationNextSteps(w, report)
}

func printMutationNextSteps(w io.Writer, report mutationReport) {
	switch report.Status {
	case plan.StatusHeld:
		if len(report.Candidates) > 0 {
			fmt.Fprintln(w, "\nnext: rerun with an explicit --provider and --kind from the candidates above")
		}
	case plan.StatusError:
		if report.RollbackCommand != "" {
			fmt.Fprintln(w, "\nnext: inspect the error, then use the rollback command above if the manifest change should be reverted")
		}
	}
}

func parseRollbackOptions(args []string) (rollbackOptions, error) {
	opts := rollbackOptions{format: "text", root: defaultRoot()}
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
		case "--token":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--token requires a value")
			}
			opts.token = args[i+1]
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

func runRollback(opts rollbackOptions) int {
	report := buildRollbackReport(opts)
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printRollbackText(os.Stdout, report)
	}
	return updateExitCode(report.Status)
}

func buildRollbackReport(opts rollbackOptions) rollbackReport {
	snap, err := snapshot.Restore(opts.root, opts.token)
	if err != nil {
		return rollbackReport{SchemaVersion: rollbackReportSchemaVersion, Status: plan.StatusError, Root: opts.root, Token: opts.token, Error: err.Error()}
	}
	files := make([]string, 0, len(snap.Files))
	for _, file := range snap.Files {
		files = append(files, file.Source)
	}
	return rollbackReport{SchemaVersion: rollbackReportSchemaVersion, Status: plan.StatusOK, Root: opts.root, Token: snap.Token, RestoredFiles: files}
}

func printRollbackText(w io.Writer, report rollbackReport) {
	fmt.Fprintf(w, "updev rollback [%s]\n", report.Status)
	fmt.Fprintf(w, "root: %s\n", report.Root)
	if report.Token != "" {
		fmt.Fprintf(w, "snapshot: %s\n", report.Token)
	}
	if report.Error != "" {
		fmt.Fprintf(w, "error: %s\n", report.Error)
	}
	if len(report.RestoredFiles) > 0 {
		fmt.Fprintln(w, "restored files:")
		for _, file := range report.RestoredFiles {
			fmt.Fprintf(w, "  %s\n", file)
		}
	}
}

func runEdit(args []string) int {
	opts, err := parseMutationOptions("edit", args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return usageExitCode
	}
	path := brewfile.SourcePath(opts.root)
	if opts.provider == "mise" || opts.kind == "tool" {
		path = mise.ConfigPath(opts.root)
	}
	before := readManifestContents([]string{path})
	snap, err := snapshot.Create(opts.root, []string{path})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	var stderr bytes.Buffer
	editorArgs := strings.Fields(editor)
	if len(editorArgs) == 0 {
		editorArgs = []string{"vi"}
	}
	command := exec.Command(editorArgs[0], append(editorArgs[1:], path)...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "editor failed: %v %s\n", err, stderr.String())
		return 1
	}
	after := readManifestContents([]string{path})
	changedFiles, diff := diffManifestContents(before, after)
	report := mutationReport{
		SchemaVersion:   mutationReportSchemaVersion,
		Status:          plan.StatusOK,
		Action:          "edit",
		Root:            opts.root,
		Changed:         len(changedFiles) > 0,
		ChangedFiles:    changedFiles,
		Diff:            diff,
		Validation:      validateAfterMutation(context.Background(), opts.root),
		Snapshot:        &snapshotRef{Token: snap.Token, Files: len(snap.Files)},
		RollbackCommand: "updev rollback --token " + snap.Token,
	}
	report.Status = statusAfterValidation(report.Status, report.Validation)
	if !report.Changed {
		report.Reason = "no manifest change was needed"
	}
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printMutationText(os.Stdout, report)
	}
	return updateExitCode(report.Status)
}
