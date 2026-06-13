package nativeaudit

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/securityadvisory"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

func ProjectPythonSitePackagesPath(root string) string {
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

func ProjectPythonRequirementPaths(root string) []string {
	return ExistingFiles([]string{
		filepath.Join(root, "requirements*.txt"),
		filepath.Join(root, "*-requirements.txt"),
		filepath.Join(root, "requirements", "*.txt"),
	})
}

func ProjectPythonLockedAuditTarget(root string) string {
	if path := ProjectLockfilePath(root, "pyproject.toml"); path != "" {
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

func ProjectGoModulePath(root string) string {
	return ProjectLockfilePath(root, "go.mod")
}

func ProjectDotnetTargets(root string) []string {
	return ExistingFiles([]string{
		filepath.Join(root, "*.sln"),
		filepath.Join(root, "*.csproj"),
	})
}

func ProjectMavenTargets(root string) []string {
	return ExistingFiles([]string{
		filepath.Join(root, "pom.xml"),
		filepath.Join(root, "build.gradle"),
		filepath.Join(root, "build.gradle.kts"),
	})
}

func CargoAuditBinaryPaths(packages []securityadvisory.Package) []string {
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

func cargoAuditPackageBinaryPaths(pkg securityadvisory.Package) []string {
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

func cargoAuditPackageBinaryNames(pkg securityadvisory.Package) []string {
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
	return CargoBinaryCandidates(pkg.Package)
}

func CargoBinaryCandidates(crate string) []string {
	if strings.ContainsAny(crate, "/@:") {
		return nil
	}
	switch crate {
	case "fd-find":
		return []string{"fd", "fd-find"}
	case "git-delta":
		return []string{"delta", "git-delta"}
	default:
		return []string{crate}
	}
}

func cargoAuditBinDirs() []string {
	dirs := []string{}
	if cargoHome := strings.TrimSpace(os.Getenv("CARGO_HOME")); cargoHome != "" {
		dirs = append(dirs, filepath.Join(cargoHome, "bin"))
	}
	if home := updevpath.HomeDir(); home != "" {
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

func PipxAuditPaths(packages []securityadvisory.Package) []string {
	seen := map[string]bool{}
	paths := []string{}
	for _, pkg := range packages {
		if !strings.EqualFold(pkg.Ecosystem, "PyPI") || !safeMiseToolPathPart(pkg.Package) || !safeMiseToolPathPart(pkg.Version) {
			continue
		}
		pattern := filepath.Join(MiseDataDir(), "installs", "pipx-"+pkg.Package, pkg.Version, pkg.Package, "lib", "python*", "site-packages")
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

func MiseDataDir() string {
	if value := strings.TrimSpace(os.Getenv("MISE_DATA_DIR")); value != "" {
		return value
	}
	if value := updevpath.DataHome(); value != "" {
		return filepath.Join(value, "mise")
	}
	return filepath.Join(".local", "share", "mise")
}

func ExistingFiles(patterns []string) []string {
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

func ProjectLockfilePath(root string, names ...string) string {
	for _, name := range names {
		path := filepath.Join(root, name)
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}
