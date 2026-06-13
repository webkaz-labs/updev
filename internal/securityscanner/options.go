package securityscanner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func Tools(value string, root string) []string {
	names, err := ParseNames(value)
	if err != nil || len(names) == 0 {
		return nil
	}
	if len(names) == 1 {
		switch names[0] {
		case "none":
			return nil
		case "auto":
			tools := []string{"osv-scanner", "gitleaks"}
			if HasGitHubWorkflowFiles(root) {
				tools = append(tools, "zizmor")
			}
			return tools
		case "all":
			return []string{"osv-scanner", "gitleaks", "zizmor", "trivy", "grype"}
		}
	}
	return names
}

func ParseNames(value string) ([]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "auto"
	}
	names := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(value, ",") {
		name := NormalizeName(part)
		if name == "" {
			continue
		}
		if !NameAllowed(name) {
			return nil, fmt.Errorf("unsupported scanner: %s", strings.TrimSpace(part))
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return []string{"auto"}, nil
	}
	if len(names) > 1 {
		for _, name := range names {
			if name == "auto" || name == "all" || name == "none" {
				return nil, fmt.Errorf("scanner %q cannot be combined with other scanners", name)
			}
		}
	}
	return names, nil
}

func NormalizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "osv":
		return "osv-scanner"
	case "secret", "secrets":
		return "gitleaks"
	case "workflow", "workflows", "actions", "github-actions":
		return "zizmor"
	case "trivy-fs":
		return "trivy"
	case "anchore-grype", "grype-dir":
		return "grype"
	default:
		return value
	}
}

func NameAllowed(name string) bool {
	switch name {
	case "auto", "none", "all", "osv-scanner", "gitleaks", "zizmor", "trivy", "grype":
		return true
	default:
		return false
	}
}

func HasGitHubWorkflowFiles(root string) bool {
	entries, err := os.ReadDir(filepath.Join(root, ".github", "workflows"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.ToLower(entry.Name())
		if strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml") {
			return true
		}
	}
	return false
}
