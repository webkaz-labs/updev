package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"sync"

	"github.com/webkaz-labs/updev/internal/nativeaudit"
	"github.com/webkaz-labs/updev/internal/plan"
)

type nativeAudit = nativeaudit.Evidence

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
	parsed, ok := nativeaudit.ParseNPMReport(result.Stdout)
	if ok {
		return nativeaudit.FromNPMReport(audit, parsed, "npm native audit unavailable", "npm native audit reported vulnerabilities")
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = plan.StatusError
		audit.Decision = "review"
		audit.Reason = "npm native audit failed"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
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
	parsed, ok := nativeaudit.ParseNPMReport(result.Stdout)
	if ok {
		return nativeaudit.FromNPMReport(audit, parsed, "npm lockfile audit unavailable", "npm lockfile audit reported vulnerabilities")
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.PackageManagerErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "npm lockfile audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
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
	parsed, ok := nativeaudit.ParseNPMReport(result.Stdout)
	if ok {
		return nativeaudit.FromNPMReport(audit, parsed, "pnpm lockfile audit unavailable", "pnpm lockfile audit reported vulnerabilities")
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.PackageManagerErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "pnpm lockfile audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
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
	parsed, ok := nativeaudit.ParseGenericReport(result.Stdout)
	if ok {
		return nativeaudit.FromGenericReport(audit, parsed, "bun lockfile audit unavailable", "bun lockfile audit reported vulnerabilities")
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.PackageManagerErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "bun lockfile audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
		return audit
	}
	return audit
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
	paths := nativeaudit.CargoAuditBinaryPaths(packages)
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
	parsed, ok := nativeaudit.ParseCargoReport(result.Stdout)
	if ok {
		return nativeaudit.FromCargoReport(audit, parsed)
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.CargoErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "cargo audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
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
	parsed, ok := nativeaudit.ParseCargoReport(result.Stdout)
	if ok {
		audit = nativeaudit.FromCargoReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.CargoErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "Cargo project audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
		return audit
	}
	return audit
}

func runPyPINativeAudit(ctx context.Context, commandRunner commandRunner, packages []securityPackage) nativeAudit {
	args := []string{"--format", "json"}
	paths := nativeaudit.PipxAuditPaths(packages)
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
	parsed, ok := nativeaudit.ParsePipReport(result.Stdout)
	if ok {
		return nativeaudit.FromPipReport(audit, parsed)
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.PipErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = unavailableReason
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
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
	parsed, ok := nativeaudit.ParseGovulncheckReport(result.Stdout)
	if ok {
		audit = nativeaudit.FromGovulncheckReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.GovulncheckErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "Go project vulnerability audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
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
	parsed, ok := nativeaudit.ParseComposerReport(result.Stdout)
	if ok {
		audit = nativeaudit.FromComposerReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.PackageManagerErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "Composer project audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
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
	parsed, ok := nativeaudit.ParseBundlerReport(result.Stdout)
	if ok {
		audit = nativeaudit.FromBundlerReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.PackageManagerErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = "Bundler project audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
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
	parsed, ok := nativeaudit.ParseDotnetReport(result.Stdout)
	if ok {
		audit = nativeaudit.FromDotnetReport(audit, parsed)
		if audit.Status == plan.StatusHeld || result.Code == 0 && result.Err == nil {
			return audit
		}
	}
	if result.Code != 0 || result.Err != nil {
		audit.Status = nativeaudit.PackageManagerErrorStatus(result)
		audit.Decision = "review"
		audit.Reason = ".NET project audit unavailable"
		audit.Error = firstNonEmpty(result.Stderr, result.Stdout, nativeaudit.ErrorText(result))
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

func projectPythonSitePackagesPath(root string) string {
	return nativeaudit.ProjectPythonSitePackagesPath(root)
}

func projectPythonRequirementPaths(root string) []string {
	return nativeaudit.ProjectPythonRequirementPaths(root)
}

func projectPythonLockedAuditTarget(root string) string {
	return nativeaudit.ProjectPythonLockedAuditTarget(root)
}

func projectGoModulePath(root string) string {
	return nativeaudit.ProjectGoModulePath(root)
}

func projectDotnetTargets(root string) []string {
	return nativeaudit.ProjectDotnetTargets(root)
}

func projectMavenTargets(root string) []string {
	return nativeaudit.ProjectMavenTargets(root)
}

func projectLockfilePath(root string, names ...string) string {
	return nativeaudit.ProjectLockfilePath(root, names...)
}

func nativeAuditReportStatus(current plan.Status, audits []nativeAudit) plan.Status {
	return nativeaudit.ReportStatus(current, audits)
}

func nativeAuditSummary(audits []nativeAudit) (held int, unavailable int, errors int) {
	return nativeaudit.Summary(audits)
}

func hasNativeAuditAttention(audits []nativeAudit) bool {
	return nativeaudit.HasAttention(audits)
}
