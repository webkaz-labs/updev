package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

type nativeAudit struct {
	Provider        string        `json:"provider"`
	Ecosystem       string        `json:"ecosystem"`
	Tool            string        `json:"tool"`
	Command         []string      `json:"command,omitempty"`
	Target          string        `json:"target,omitempty"`
	Status          plan.Status   `json:"status"`
	Decision        string        `json:"decision"`
	Reason          string        `json:"reason,omitempty"`
	AdvisoryCount   int           `json:"advisory_count,omitempty"`
	Vulnerabilities *nativeCounts `json:"vulnerabilities,omitempty"`
	Error           string        `json:"error,omitempty"`
}

type nativeCounts struct {
	Info     int `json:"info,omitempty"`
	Low      int `json:"low,omitempty"`
	Moderate int `json:"moderate,omitempty"`
	High     int `json:"high,omitempty"`
	Critical int `json:"critical,omitempty"`
	Total    int `json:"total,omitempty"`
}

type npmAuditReport struct {
	Error           npmAuditError              `json:"error"`
	Vulnerabilities map[string]json.RawMessage `json:"vulnerabilities"`
	Metadata        struct {
		Vulnerabilities nativeCounts `json:"vulnerabilities"`
	} `json:"metadata"`
}

type npmAuditError struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	Detail  string `json:"detail"`
}

type cargoAuditReport struct {
	Vulnerabilities struct {
		Found bool              `json:"found"`
		Count int               `json:"count"`
		List  []json.RawMessage `json:"list"`
	} `json:"vulnerabilities"`
}

type pipAuditReport struct {
	Dependencies []pipAuditDependency `json:"dependencies"`
}

type pipAuditDependency struct {
	Name    string            `json:"name"`
	Version string            `json:"version"`
	Vulns   []json.RawMessage `json:"vulns"`
}

type govulncheckMessage struct {
	Finding *govulncheckFinding `json:"finding"`
}

type govulncheckFinding struct {
	OSV          string `json:"osv"`
	FixedVersion string `json:"fixed_version"`
}

type composerAuditReport struct {
	Advisories json.RawMessage `json:"advisories"`
}

type bundlerAuditReport struct {
	Results         json.RawMessage `json:"results"`
	Vulnerabilities json.RawMessage `json:"vulnerabilities"`
	Advisories      json.RawMessage `json:"advisories"`
}

type dotnetAuditReport struct {
	Projects []dotnetAuditProject `json:"projects"`
}

type dotnetAuditProject struct {
	Frameworks []dotnetAuditFramework `json:"frameworks"`
}

type dotnetAuditFramework struct {
	TopLevelPackages   []dotnetAuditPackage `json:"topLevelPackages"`
	TransitivePackages []dotnetAuditPackage `json:"transitivePackages"`
}

type dotnetAuditPackage struct {
	Vulnerabilities []json.RawMessage `json:"vulnerabilities"`
}

func nativeAuditsFromPackages(ctx context.Context, commandRunner commandRunner, packages []securityPackage, opts securityOptions) []nativeAudit {
	tasks := []func() nativeAudit{}
	if nativeAuditShouldRun(opts, "npm") && securityPackagesIncludeEcosystem(packages, "npm") {
		tasks = append(tasks, func() nativeAudit {
			return runNPMNativeAudit(ctx, commandRunner)
		})
	}
	if nativeProjectAuditShouldRun(opts, "npm") {
		if lockfile := projectLockfilePath(opts.root, "package-lock.json", "npm-shrinkwrap.json"); lockfile != "" {
			tasks = append(tasks, func() nativeAudit {
				return runNPMLockfileNativeAudit(ctx, commandRunner, opts.root, lockfile)
			})
		}
		if lockfile := projectLockfilePath(opts.root, "pnpm-lock.yaml"); lockfile != "" {
			tasks = append(tasks, func() nativeAudit {
				return runPNPMLockfileNativeAudit(ctx, commandRunner, opts.root, lockfile)
			})
		}
		if lockfile := projectLockfilePath(opts.root, "bun.lock", "bun.lockb"); lockfile != "" {
			tasks = append(tasks, func() nativeAudit {
				return runBunLockfileNativeAudit(ctx, commandRunner, opts.root, lockfile)
			})
		}
	}
	if nativeProjectAuditShouldRun(opts, "crates.io") {
		if lockfile := projectLockfilePath(opts.root, "Cargo.lock"); lockfile != "" {
			tasks = append(tasks, func() nativeAudit {
				return runCargoProjectNativeAudit(ctx, commandRunner, opts.root, lockfile)
			})
		}
	}
	if nativeAuditShouldRun(opts, "crates.io") {
		if securityPackagesIncludeEcosystem(packages, "crates.io") {
			tasks = append(tasks, func() nativeAudit {
				return runCargoNativeAudit(ctx, commandRunner, packages)
			})
		}
	}
	if nativeAuditShouldRun(opts, "PyPI") {
		if securityPackagesIncludeEcosystem(packages, "PyPI") {
			tasks = append(tasks, func() nativeAudit {
				return runPyPINativeAudit(ctx, commandRunner, packages)
			})
		}
	}
	if nativeProjectAuditShouldRun(opts, "PyPI") {
		for _, requirements := range projectPythonRequirementPaths(opts.root) {
			requirements := requirements
			tasks = append(tasks, func() nativeAudit {
				return runPythonRequirementsNativeAudit(ctx, commandRunner, requirements)
			})
		}
		if target := projectPythonLockedAuditTarget(opts.root); target != "" {
			tasks = append(tasks, func() nativeAudit {
				return runPythonLockedProjectNativeAudit(ctx, commandRunner, opts.root, target)
			})
		}
		if sitePackages := projectPythonSitePackagesPath(opts.root); sitePackages != "" {
			tasks = append(tasks, func() nativeAudit {
				return runPythonProjectNativeAudit(ctx, commandRunner, sitePackages)
			})
		}
	}
	if nativeProjectAuditShouldRun(opts, "Go") {
		if module := projectGoModulePath(opts.root); module != "" {
			tasks = append(tasks, func() nativeAudit {
				return runGoProjectNativeAudit(ctx, commandRunner, opts.root, module)
			})
		}
	}
	if nativeProjectAuditShouldRun(opts, "Packagist") {
		if lockfile := projectLockfilePath(opts.root, "composer.lock"); lockfile != "" {
			tasks = append(tasks, func() nativeAudit {
				return runComposerProjectNativeAudit(ctx, commandRunner, opts.root, lockfile)
			})
		}
	}
	if nativeProjectAuditShouldRun(opts, "RubyGems") {
		if lockfile := projectLockfilePath(opts.root, "Gemfile.lock"); lockfile != "" {
			tasks = append(tasks, func() nativeAudit {
				return runBundlerProjectNativeAudit(ctx, commandRunner, lockfile)
			})
		}
	}
	if nativeProjectAuditShouldRun(opts, "NuGet") {
		for _, target := range projectDotnetTargets(opts.root) {
			target := target
			tasks = append(tasks, func() nativeAudit {
				return runDotnetProjectNativeAudit(ctx, commandRunner, target)
			})
		}
	}
	if nativeProjectAuditShouldRun(opts, "Maven") {
		for _, target := range projectMavenTargets(opts.root) {
			target := target
			tasks = append(tasks, func() nativeAudit {
				return mavenProjectNativeAuditUnavailable(target)
			})
		}
	}
	return runNativeAuditTasks(tasks)
}

func runNativeAuditTasks(tasks []func() nativeAudit) []nativeAudit {
	if len(tasks) == 0 {
		return nil
	}
	audits := make([]nativeAudit, len(tasks))
	var wg sync.WaitGroup
	for index, task := range tasks {
		wg.Add(1)
		go func(index int, task func() nativeAudit) {
			defer wg.Done()
			audits[index] = task()
		}(index, task)
	}
	wg.Wait()
	return audits
}

func nativeAuditShouldRun(opts securityOptions, ecosystem string) bool {
	return opts.ecosystem == "" || strings.EqualFold(opts.ecosystem, ecosystem)
}

func nativeProjectAuditShouldRun(opts securityOptions, ecosystem string) bool {
	return securityScanIncludesProjectProvider(opts.provider) && nativeAuditShouldRun(opts, ecosystem)
}

func securityPackagesIncludeEcosystem(packages []securityPackage, ecosystem string) bool {
	for _, pkg := range packages {
		if strings.EqualFold(pkg.Ecosystem, ecosystem) {
			return true
		}
	}
	return false
}

func runNPMNativeAudit(ctx context.Context, commandRunner commandRunner) nativeAudit {
	audit := nativeAudit{
		Provider:  "mise",
		Ecosystem: "npm",
		Tool:      "npm",
		Command:   []string{"npm", "audit", "--json", "--global"},
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    "npm native audit completed",
	}
	result := commandRunner.Run(ctx, "npm", "audit", "--json", "--global")
	parsed, ok := parseNPMAuditReport(result.Stdout)
	if ok {
		return npmNativeAuditFromReport(audit, parsed, "npm native audit unavailable", "npm native audit reported vulnerabilities")
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = plan.StatusError
		audit.Decision = "review"
		audit.Reason = "npm native audit failed"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func runNPMLockfileNativeAudit(ctx context.Context, commandRunner commandRunner, root string, lockfile string) nativeAudit {
	args := []string{"audit", "--json", "--prefix", root}
	audit := nativeAudit{
		Provider:  "project",
		Ecosystem: "npm",
		Tool:      "npm",
		Command:   append([]string{"npm"}, args...),
		Target:    lockfile,
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    "npm lockfile audit completed",
	}
	result := commandRunner.Run(ctx, "npm", args...)
	parsed, ok := parseNPMAuditReport(result.Stdout)
	if ok {
		return npmNativeAuditFromReport(audit, parsed, "npm lockfile audit unavailable", "npm lockfile audit reported vulnerabilities")
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = packageManagerAuditErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "npm lockfile audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func runPNPMLockfileNativeAudit(ctx context.Context, commandRunner commandRunner, root string, lockfile string) nativeAudit {
	args := []string{"--dir", root, "audit", "--json"}
	audit := nativeAudit{
		Provider:  "project",
		Ecosystem: "npm",
		Tool:      "pnpm",
		Command:   append([]string{"pnpm"}, args...),
		Target:    lockfile,
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    "pnpm lockfile audit completed",
	}
	result := commandRunner.Run(ctx, "pnpm", args...)
	parsed, ok := parseNPMAuditReport(result.Stdout)
	if ok {
		return npmNativeAuditFromReport(audit, parsed, "pnpm lockfile audit unavailable", "pnpm lockfile audit reported vulnerabilities")
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = packageManagerAuditErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "pnpm lockfile audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func runBunLockfileNativeAudit(ctx context.Context, commandRunner commandRunner, root string, lockfile string) nativeAudit {
	args := []string{"audit", "--json", "--cwd", root}
	audit := nativeAudit{
		Provider:  "project",
		Ecosystem: "npm",
		Tool:      "bun",
		Command:   append([]string{"bun"}, args...),
		Target:    lockfile,
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    "bun lockfile audit completed",
	}
	result := commandRunner.Run(ctx, "bun", args...)
	parsed, ok := parseGenericNativeAuditReport(result.Stdout)
	if ok {
		return genericNativeAuditFromReport(audit, parsed, "bun lockfile audit unavailable", "bun lockfile audit reported vulnerabilities")
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = packageManagerAuditErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "bun lockfile audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func parseNPMAuditReport(raw string) (npmAuditReport, bool) {
	var report npmAuditReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func npmNativeAuditFromReport(audit nativeAudit, report npmAuditReport, unavailableReason string, vulnerableReason string) nativeAudit {
	if report.Error.Code != "" {
		audit.Status = npmAuditErrorStatus(report.Error.Code)
		audit.Decision = "review"
		audit.Reason = firstNonEmpty(report.Error.Summary, unavailableReason)
		audit.Error = firstNonEmpty(report.Error.Detail, report.Error.Code)
		return audit
	}
	audit.AdvisoryCount = len(report.Vulnerabilities)
	counts := report.Metadata.Vulnerabilities
	if counts.Total == 0 {
		counts.Total = audit.AdvisoryCount
	}
	if counts.Total > 0 {
		audit.Vulnerabilities = &counts
	}
	if audit.AdvisoryCount > 0 || counts.Total > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		audit.Reason = vulnerableReason
		return audit
	}
	return audit
}

type genericNativeAuditReport struct {
	Error      npmAuditError
	Advisories int
	Vulnerable nativeCounts
}

func parseGenericNativeAuditReport(raw string) (genericNativeAuditReport, bool) {
	var report genericNativeAuditReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return report, false
	}
	if rawError, ok := root["error"]; ok {
		_ = json.Unmarshal(rawError, &report.Error)
	}
	report.Advisories += countJSONEntries(root["vulnerabilities"])
	report.Advisories += countJSONEntries(root["advisories"])
	if rawMetadata, ok := root["metadata"]; ok {
		var metadata struct {
			Vulnerabilities nativeCounts `json:"vulnerabilities"`
		}
		if err := json.Unmarshal(rawMetadata, &metadata); err == nil {
			report.Vulnerable = metadata.Vulnerabilities
		}
	}
	return report, true
}

func genericNativeAuditFromReport(audit nativeAudit, report genericNativeAuditReport, unavailableReason string, vulnerableReason string) nativeAudit {
	if report.Error.Code != "" {
		audit.Status = npmAuditErrorStatus(report.Error.Code)
		audit.Decision = "review"
		audit.Reason = firstNonEmpty(report.Error.Summary, unavailableReason)
		audit.Error = firstNonEmpty(report.Error.Detail, report.Error.Code)
		return audit
	}
	counts := report.Vulnerable
	if counts.Total == 0 {
		counts.Total = report.Advisories
	}
	audit.AdvisoryCount = report.Advisories
	if counts.Total > 0 {
		audit.Vulnerabilities = &counts
	}
	if audit.AdvisoryCount > 0 || counts.Total > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		audit.Reason = vulnerableReason
	}
	return audit
}

func countJSONEntries(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var many []json.RawMessage
	if err := json.Unmarshal(raw, &many); err == nil {
		return len(many)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		return len(object)
	}
	return 0
}

func npmAuditErrorStatus(code string) plan.Status {
	if strings.EqualFold(code, "EAUDITGLOBAL") || strings.EqualFold(code, "ENOLOCK") {
		return plan.StatusUnavailable
	}
	return plan.StatusError
}

func packageManagerAuditErrorStatus(result runner.Result) plan.Status {
	detail := strings.ToLower(firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result)))
	if strings.Contains(detail, "no package.json") ||
		strings.Contains(detail, "no lockfile") ||
		strings.Contains(detail, "lockfile not found") ||
		strings.Contains(detail, "executable file not found") ||
		strings.Contains(detail, "command not found") ||
		strings.Contains(detail, "file does not exist") ||
		strings.Contains(detail, "no such file") {
		return plan.StatusUnavailable
	}
	return plan.StatusError
}

func runCargoNativeAudit(ctx context.Context, commandRunner commandRunner, packages []securityPackage) nativeAudit {
	args := []string{"audit", "--json", "bin"}
	audit := nativeAudit{
		Provider:  "mise",
		Ecosystem: "crates.io",
		Tool:      "cargo-audit",
		Command:   append([]string{"cargo"}, args...),
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    "cargo audit completed",
	}
	paths := cargoAuditBinaryPaths(packages)
	if len(paths) == 0 {
		audit.Status = plan.StatusUnavailable
		audit.Decision = "review"
		audit.Reason = "cargo audit unavailable"
		audit.Error = "no cargo-installed binaries found for binary audit"
		return audit
	}
	args = append(args, paths...)
	audit.Command = append([]string{"cargo"}, args...)
	result := commandRunner.Run(ctx, "cargo", args...)
	parsed, ok := parseCargoAuditReport(result.Stdout)
	if ok {
		return cargoNativeAuditFromReport(audit, parsed)
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = cargoAuditErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "cargo audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func runCargoProjectNativeAudit(ctx context.Context, commandRunner commandRunner, root string, lockfile string) nativeAudit {
	args := []string{"-lc", "cd \"$1\" && cargo audit --json", "updev-cargo-audit", root}
	audit := nativeAudit{
		Provider:  "project",
		Ecosystem: "crates.io",
		Tool:      "cargo-audit",
		Command:   append([]string{"bash"}, args...),
		Target:    lockfile,
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    "Cargo project audit completed",
	}
	result := commandRunner.Run(ctx, "bash", args...)
	parsed, ok := parseCargoAuditReport(result.Stdout)
	if ok {
		audit = cargoNativeAuditFromReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = cargoAuditErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "Cargo project audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func cargoAuditBinaryPaths(packages []securityPackage) []string {
	seen := map[string]bool{}
	paths := []string{}
	for _, pkg := range packages {
		if !strings.EqualFold(pkg.Ecosystem, "crates.io") {
			continue
		}
		for _, path := range cargoAuditPackageBinaryPaths(pkg) {
			if seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func cargoAuditPackageBinaryPaths(pkg securityPackage) []string {
	if pkg.PathState == "on-path" && pkg.BinaryPath != "" {
		return []string{pkg.BinaryPath}
	}
	names := cargoAuditPackageBinaryNames(pkg)
	if len(names) == 0 {
		return nil
	}
	paths := []string{}
	for _, dir := range cargoAuditBinDirs() {
		for _, name := range names {
			path := filepath.Join(dir, name)
			if !cargoAuditBinaryExists(path) {
				continue
			}
			paths = append(paths, path)
		}
	}
	return paths
}

func cargoAuditPackageBinaryNames(pkg securityPackage) []string {
	if pkg.BinaryName != "" {
		names := []string{}
		for _, name := range strings.Split(pkg.BinaryName, ",") {
			name = strings.TrimSpace(name)
			if name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			return names
		}
	}
	return cargoBinaryCandidates(pkg.Package)
}

func cargoAuditBinDirs() []string {
	dirs := []string{}
	if cargoHome := strings.TrimSpace(os.Getenv("CARGO_HOME")); cargoHome != "" {
		dirs = append(dirs, filepath.Join(cargoHome, "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		dirs = append(dirs, filepath.Join(home, ".cargo", "bin"))
	}
	out := []string{}
	seen := map[string]bool{}
	for _, dir := range dirs {
		cleaned := filepath.Clean(dir)
		if cleaned == "." || seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
	}
	return out
}

func cargoAuditBinaryExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func parseCargoAuditReport(raw string) (cargoAuditReport, bool) {
	var report cargoAuditReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func cargoNativeAuditFromReport(audit nativeAudit, report cargoAuditReport) nativeAudit {
	count := report.Vulnerabilities.Count
	if count == 0 {
		count = len(report.Vulnerabilities.List)
	}
	if report.Vulnerabilities.Found || count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		audit.Reason = "cargo audit reported vulnerabilities"
		audit.AdvisoryCount = count
		counts := nativeCounts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func cargoAuditErrorStatus(result runner.Result) plan.Status {
	detail := strings.ToLower(firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result)))
	if strings.Contains(detail, "no such command") ||
		strings.Contains(detail, "no such subcommand") ||
		strings.Contains(detail, "install cargo-audit") ||
		strings.Contains(detail, "cargo.lock") {
		return plan.StatusUnavailable
	}
	return plan.StatusError
}

func runPyPINativeAudit(ctx context.Context, commandRunner commandRunner, packages []securityPackage) nativeAudit {
	args := []string{"--format", "json"}
	paths := pipxAuditPaths(packages)
	if len(paths) == 0 {
		return nativeAudit{
			Provider:  "mise",
			Ecosystem: "PyPI",
			Tool:      "pip-audit",
			Command:   append([]string{"pip-audit"}, args...),
			Status:    plan.StatusUnavailable,
			Decision:  "review",
			Reason:    "pip-audit unavailable",
			Error:     "no mise pipx site-packages paths found for audit",
		}
	}
	for _, path := range paths {
		args = append(args, "--path", path)
	}
	return runPipAuditNative(ctx, commandRunner, "mise", "", args, "pip-audit completed", "pip-audit unavailable")
}

func runPythonProjectNativeAudit(ctx context.Context, commandRunner commandRunner, sitePackages string) nativeAudit {
	args := []string{"--format", "json", "--path", sitePackages}
	return runPipAuditNative(ctx, commandRunner, "project", sitePackages, args, "Python project audit completed", "Python project audit unavailable")
}

func runPythonRequirementsNativeAudit(ctx context.Context, commandRunner commandRunner, requirements string) nativeAudit {
	args := []string{"--format", "json", "--requirement", requirements}
	return runPipAuditNative(ctx, commandRunner, "project", requirements, args, "Python requirements audit completed", "Python requirements audit unavailable")
}

func runPythonLockedProjectNativeAudit(ctx context.Context, commandRunner commandRunner, root string, target string) nativeAudit {
	args := []string{"--format", "json", "--locked", root}
	return runPipAuditNative(ctx, commandRunner, "project", target, args, "Python locked project audit completed", "Python locked project audit unavailable")
}

func runPipAuditNative(ctx context.Context, commandRunner commandRunner, provider string, target string, args []string, completedReason string, unavailableReason string) nativeAudit {
	audit := nativeAudit{
		Provider:  provider,
		Ecosystem: "PyPI",
		Tool:      "pip-audit",
		Command:   append([]string{"pip-audit"}, args...),
		Target:    target,
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    completedReason,
	}
	result := commandRunner.Run(ctx, "pip-audit", args...)
	parsed, ok := parsePipAuditReport(result.Stdout)
	if ok {
		return pipNativeAuditFromReport(audit, parsed)
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = pipAuditErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = unavailableReason
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func runGoProjectNativeAudit(ctx context.Context, commandRunner commandRunner, root string, module string) nativeAudit {
	pattern := filepath.Join(root, "...")
	args := []string{"-format=json", pattern}
	audit := nativeAudit{
		Provider:  "project",
		Ecosystem: "Go",
		Tool:      "govulncheck",
		Command:   append([]string{"govulncheck"}, args...),
		Target:    module,
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    "Go project vulnerability audit completed",
	}
	result := commandRunner.Run(ctx, "govulncheck", args...)
	parsed, ok := parseGovulncheckReport(result.Stdout)
	if ok {
		audit = goNativeAuditFromReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = govulncheckErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "Go project vulnerability audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func runComposerProjectNativeAudit(ctx context.Context, commandRunner commandRunner, root string, lockfile string) nativeAudit {
	args := []string{"--working-dir", root, "audit", "--format=json", "--locked", "--no-interaction"}
	audit := nativeAudit{
		Provider:  "project",
		Ecosystem: "Packagist",
		Tool:      "composer",
		Command:   append([]string{"composer"}, args...),
		Target:    lockfile,
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    "Composer project audit completed",
	}
	result := commandRunner.Run(ctx, "composer", args...)
	parsed, ok := parseComposerAuditReport(result.Stdout)
	if ok {
		audit = composerNativeAuditFromReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = packageManagerAuditErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "Composer project audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func runBundlerProjectNativeAudit(ctx context.Context, commandRunner commandRunner, lockfile string) nativeAudit {
	args := []string{"check", "--format", "json", "--gemfile", lockfile}
	audit := nativeAudit{
		Provider:  "project",
		Ecosystem: "RubyGems",
		Tool:      "bundle-audit",
		Command:   append([]string{"bundle-audit"}, args...),
		Target:    lockfile,
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    "Bundler project audit completed",
	}
	result := commandRunner.Run(ctx, "bundle-audit", args...)
	parsed, ok := parseBundlerAuditReport(result.Stdout)
	if ok {
		audit = bundlerNativeAuditFromReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = packageManagerAuditErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "Bundler project audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func runDotnetProjectNativeAudit(ctx context.Context, commandRunner commandRunner, target string) nativeAudit {
	args := []string{"package", "list", target, "--include-transitive", "--vulnerable", "--format", "json"}
	audit := nativeAudit{
		Provider:  "project",
		Ecosystem: "NuGet",
		Tool:      "dotnet",
		Command:   append([]string{"dotnet"}, args...),
		Target:    target,
		Status:    plan.StatusOK,
		Decision:  "allow",
		Reason:    ".NET project audit completed",
	}
	result := commandRunner.Run(ctx, "dotnet", args...)
	parsed, ok := parseDotnetAuditReport(result.Stdout)
	if ok {
		audit = dotnetNativeAuditFromReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = packageManagerAuditErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = ".NET project audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result))
		return audit
	}
	return audit
}

func mavenProjectNativeAuditUnavailable(target string) nativeAudit {
	return nativeAudit{
		Provider:  "project",
		Ecosystem: "Maven",
		Tool:      "maven-native-audit",
		Target:    target,
		Status:    plan.StatusUnavailable,
		Decision:  "review",
		Reason:    "Maven project audit unavailable",
		Error:     "no configured provider-native Maven vulnerability audit; use OSV-Scanner, Trivy, Grype, or a reviewed Maven/Gradle audit tool for this project",
	}
}

func pipxAuditPaths(packages []securityPackage) []string {
	seen := map[string]bool{}
	paths := []string{}
	for _, pkg := range packages {
		if !strings.EqualFold(pkg.Ecosystem, "PyPI") || !safeMiseToolPathPart(pkg.Package) || !safeMiseToolPathPart(pkg.Version) {
			continue
		}
		pattern := filepath.Join(miseDataDir(), "installs", "pipx-"+pkg.Package, pkg.Version, pkg.Package, "lib", "python*", "site-packages")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			if seen[path] {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func projectPythonSitePackagesPath(root string) string {
	for _, envDir := range []string{".venv", "venv"} {
		pattern := filepath.Join(root, envDir, "lib", "python*", "site-packages")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			info, err := os.Stat(path)
			if err == nil && info.IsDir() {
				return path
			}
		}
	}
	return ""
}

func projectPythonRequirementPaths(root string) []string {
	patterns := []string{
		filepath.Join(root, "requirements*.txt"),
		filepath.Join(root, "*-requirements.txt"),
		filepath.Join(root, "requirements", "*.txt"),
	}
	seen := map[string]bool{}
	paths := []string{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			if seen[path] {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			seen[path] = true
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func projectPythonLockedAuditTarget(root string) string {
	if path := projectLockfilePath(root, "pyproject.toml"); path != "" {
		return path
	}
	matches, err := filepath.Glob(filepath.Join(root, "pylock.*.toml"))
	if err != nil {
		return ""
	}
	sort.Strings(matches)
	for _, path := range matches {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func projectGoModulePath(root string) string {
	return projectLockfilePath(root, "go.mod")
}

func projectDotnetTargets(root string) []string {
	return projectExistingFiles([]string{
		filepath.Join(root, "*.sln"),
		filepath.Join(root, "*.csproj"),
	})
}

func projectMavenTargets(root string) []string {
	return projectExistingFiles([]string{
		filepath.Join(root, "pom.xml"),
		filepath.Join(root, "build.gradle"),
		filepath.Join(root, "build.gradle.kts"),
	})
}

func projectExistingFiles(patterns []string) []string {
	seen := map[string]bool{}
	targets := []string{}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			if seen[path] {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			seen[path] = true
			targets = append(targets, path)
		}
	}
	sort.Strings(targets)
	return targets
}

func safeMiseToolPathPart(value string) bool {
	if value == "" || value == "." || value == ".." {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func miseDataDir() string {
	if value := strings.TrimSpace(os.Getenv("MISE_DATA_DIR")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); value != "" {
		return filepath.Join(value, "mise")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".local", "share", "mise")
	}
	return filepath.Join(home, ".local", "share", "mise")
}

func projectLockfilePath(root string, names ...string) string {
	for _, name := range names {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func parsePipAuditReport(raw string) (pipAuditReport, bool) {
	var report pipAuditReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func pipNativeAuditFromReport(audit nativeAudit, report pipAuditReport) nativeAudit {
	count := 0
	for _, dependency := range report.Dependencies {
		count += len(dependency.Vulns)
	}
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		audit.Reason = "pip-audit reported vulnerabilities"
		audit.AdvisoryCount = count
		counts := nativeCounts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func pipAuditErrorStatus(result runner.Result) plan.Status {
	detail := strings.ToLower(firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result)))
	if strings.Contains(detail, "executable file not found") ||
		strings.Contains(detail, "command not found") ||
		strings.Contains(detail, "file does not exist") ||
		strings.Contains(detail, "no such file") {
		return plan.StatusUnavailable
	}
	return plan.StatusError
}

type govulncheckReport struct {
	Findings []govulncheckFinding
}

func parseGovulncheckReport(raw string) (govulncheckReport, bool) {
	var report govulncheckReport
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return report, false
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	for {
		var message govulncheckMessage
		if err := decoder.Decode(&message); err != nil {
			if err == io.EOF {
				break
			}
			return report, false
		}
		if message.Finding == nil || strings.TrimSpace(message.Finding.OSV) == "" {
			continue
		}
		report.Findings = append(report.Findings, *message.Finding)
	}
	return report, true
}

func goNativeAuditFromReport(audit nativeAudit, report govulncheckReport) nativeAudit {
	seen := map[string]bool{}
	count := 0
	for _, finding := range report.Findings {
		id := strings.TrimSpace(finding.OSV)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		count++
	}
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		audit.Reason = "govulncheck reported vulnerabilities"
		audit.AdvisoryCount = count
		counts := nativeCounts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func parseComposerAuditReport(raw string) (composerAuditReport, bool) {
	var report composerAuditReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func composerNativeAuditFromReport(audit nativeAudit, report composerAuditReport) nativeAudit {
	count := countComposerAdvisories(report.Advisories)
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		audit.Reason = "composer audit reported vulnerabilities"
		audit.AdvisoryCount = count
		counts := nativeCounts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func countComposerAdvisories(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var grouped map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &grouped); err == nil {
		count := 0
		for _, advisories := range grouped {
			count += len(advisories)
		}
		return count
	}
	return countJSONEntries(raw)
}

func parseBundlerAuditReport(raw string) (bundlerAuditReport, bool) {
	var report bundlerAuditReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func bundlerNativeAuditFromReport(audit nativeAudit, report bundlerAuditReport) nativeAudit {
	count := countBundlerAuditFindings(report)
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		audit.Reason = "bundle-audit reported vulnerabilities"
		audit.AdvisoryCount = count
		counts := nativeCounts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func countBundlerAuditFindings(report bundlerAuditReport) int {
	for _, raw := range []json.RawMessage{report.Results, report.Vulnerabilities, report.Advisories} {
		if count := countJSONEntries(raw); count > 0 {
			return count
		}
	}
	return 0
}

func parseDotnetAuditReport(raw string) (dotnetAuditReport, bool) {
	var report dotnetAuditReport
	if strings.TrimSpace(raw) == "" {
		return report, false
	}
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return report, false
	}
	return report, true
}

func dotnetNativeAuditFromReport(audit nativeAudit, report dotnetAuditReport) nativeAudit {
	count := 0
	for _, project := range report.Projects {
		for _, framework := range project.Frameworks {
			for _, pkg := range framework.TopLevelPackages {
				count += len(pkg.Vulnerabilities)
			}
			for _, pkg := range framework.TransitivePackages {
				count += len(pkg.Vulnerabilities)
			}
		}
	}
	if count > 0 {
		audit.Status = plan.StatusHeld
		audit.Decision = "hold"
		audit.Reason = "dotnet package list reported vulnerabilities"
		audit.AdvisoryCount = count
		counts := nativeCounts{Total: count}
		audit.Vulnerabilities = &counts
	}
	return audit
}

func govulncheckErrorStatus(result runner.Result) plan.Status {
	detail := strings.ToLower(firstNonEmpty(result.Stderr, result.Stdout, nativeAuditErrorText(result)))
	if strings.Contains(detail, "executable file not found") ||
		strings.Contains(detail, "command not found") ||
		strings.Contains(detail, "file does not exist") ||
		strings.Contains(detail, "no such file") {
		return plan.StatusUnavailable
	}
	return plan.StatusError
}

func nativeAuditReportStatus(current plan.Status, audits []nativeAudit) plan.Status {
	for _, audit := range audits {
		switch audit.Status {
		case plan.StatusError:
			return plan.StatusError
		case plan.StatusBlocked:
			return plan.StatusBlocked
		case plan.StatusHeld:
			if current != plan.StatusError && current != plan.StatusBlocked {
				current = plan.StatusHeld
			}
		}
	}
	return current
}

func nativeAuditSummary(audits []nativeAudit) (held int, unavailable int, errors int) {
	for _, audit := range audits {
		switch audit.Status {
		case plan.StatusHeld:
			held++
		case plan.StatusUnavailable:
			unavailable++
		case plan.StatusError:
			errors++
		}
	}
	return held, unavailable, errors
}

func hasNativeAuditAttention(audits []nativeAudit) bool {
	for _, audit := range audits {
		if audit.Status != plan.StatusOK || securityDecisionNeedsAttention(audit.Decision) {
			return true
		}
	}
	return false
}

func nativeAuditErrorText(result runner.Result) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	if result.Code != 0 {
		return fmt.Sprintf("command exited with status %d", result.Code)
	}
	return "unknown error"
}
