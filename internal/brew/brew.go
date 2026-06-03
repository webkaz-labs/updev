package brew

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

type Provider struct {
	Root           string
	Runner         runner.Runner
	IncludeVSCode  bool
	UseHomeDesired bool
}

func (Provider) Name() string { return "brew" }

func (p Provider) Supported(ctx context.Context) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	_, err := p.Runner.LookPath("brew")
	return err == nil
}

func (p Provider) Desired(ctx context.Context) ([]plan.Item, error) {
	path := p.desiredPath()
	items, err := DesiredFromPath(path)
	if err != nil {
		return nil, err
	}
	if p.IncludeVSCode {
		return items, nil
	}
	out := make([]plan.Item, 0, len(items))
	for _, item := range items {
		if item.Kind == "vscode" {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (p Provider) desiredPath() string {
	sourcePath := filepath.Join(p.Root, "Brewfile.tmpl")
	if !p.UseHomeDesired {
		return sourcePath
	}
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, "Brewfile")
	if _, err := os.Stat(path); err != nil {
		path = sourcePath
	}
	return path
}

func DesiredFromPath(path string) ([]plan.Item, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	lineRe := regexp.MustCompile(`^\s*(brew|cask|tap|vscode)\s+"([^"]+)"`)
	items := []plan.Item{}
	category := "work"
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if nextCategory := brewCategoryFromComment(line); nextCategory != "" {
			category = nextCategory
		}
		match := lineRe.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		kind := match[1]
		name := normalizeName(kind, match[2])
		items = append(items, plan.Item{Provider: "brew", Kind: kind, Name: name, Category: category, Detail: trailingComment(line)})
	}
	return items, scanner.Err()
}

func (p Provider) Live(ctx context.Context) ([]plan.Item, error) {
	items := []plan.Item{}
	seen := map[string]bool{}
	addItem := func(kind string, name string) {
		item := plan.Item{Provider: p.Name(), Kind: kind, Name: normalizeName(kind, name)}
		key := item.Kind + "\x00" + item.Name
		if seen[key] {
			return
		}
		seen[key] = true
		items = append(items, item)
	}

	desiredFormulae := p.desiredFormulae(ctx)
	installedFormulae, _ := p.installedFormulae(ctx)
	requestedFormulae, ok := p.installedOnRequestFormulae(ctx, installedFormulae)
	if ok {
		for name := range requestedFormulae {
			addItem("brew", name)
		}
		for name := range desiredFormulae {
			if installedFormulae[name] {
				addItem("brew", name)
			}
		}
	} else {
		leafResult := p.Runner.Run(ctx, "brew", "leaves")
		if leafResult.Err == nil {
			for _, name := range splitLines(leafResult.Stdout) {
				addItem("brew", name)
			}
			for name := range desiredFormulae {
				if installedFormulae[name] {
					addItem("brew", name)
				}
			}
		} else {
			for name := range installedFormulae {
				addItem("brew", name)
			}
		}
	}

	tapInfo, _ := desiredTapInfoFromPath(p.desiredPath())
	for _, query := range []struct {
		kind string
		args []string
	}{
		{kind: "cask", args: []string{"list", "--cask", "-1"}},
		{kind: "tap", args: []string{"tap"}},
	} {
		result := p.Runner.Run(ctx, "brew", query.args...)
		if result.Err != nil {
			continue
		}
		for _, name := range splitLines(result.Stdout) {
			if query.kind == "tap" && tapInfo.IsImplicitOnly(name) {
				continue
			}
			addItem(query.kind, name)
		}
	}
	if p.IncludeVSCode {
		if _, err := p.Runner.LookPath("code"); err != nil {
			return items, nil
		}
		result := p.Runner.Run(ctx, "code", "--list-extensions")
		if result.Err == nil {
			for _, name := range splitLines(result.Stdout) {
				addItem("vscode", name)
			}
		}
	}
	return items, nil
}

func (p Provider) ExplicitFormulae(ctx context.Context) ([]string, error) {
	installedFormulae, err := p.installedFormulae(ctx)
	if err != nil {
		return nil, err
	}
	requestedFormulae, ok := p.installedOnRequestFormulae(ctx, installedFormulae)
	if !ok {
		requestedFormulae = map[string]bool{}
		leafResult := p.Runner.Run(ctx, "brew", "leaves")
		if leafResult.Err != nil {
			return nil, leafResult.Err
		}
		for _, name := range splitLines(leafResult.Stdout) {
			requestedFormulae[normalizeName("brew", name)] = true
		}
	}
	names := make([]string, 0, len(requestedFormulae))
	for name := range requestedFormulae {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (p Provider) installedFormulae(ctx context.Context) (map[string]bool, error) {
	result := p.Runner.Run(ctx, "brew", "list", "--formula", "-1")
	if result.Err != nil {
		return nil, result.Err
	}
	formulae := map[string]bool{}
	for _, name := range splitLines(result.Stdout) {
		formulae[normalizeName("brew", name)] = true
	}
	return formulae, nil
}

type desiredTapInfo struct {
	Explicit map[string]bool
	Implicit map[string]bool
}

func (info desiredTapInfo) IsImplicitOnly(name string) bool {
	name = normalizeName("tap", name)
	return info.Implicit[name] && !info.Explicit[name]
}

func desiredTapInfoFromPath(path string) (desiredTapInfo, error) {
	info := desiredTapInfo{Explicit: map[string]bool{}, Implicit: map[string]bool{}}
	file, err := os.Open(path)
	if err != nil {
		return info, err
	}
	defer file.Close()
	lineRe := regexp.MustCompile(`^\s*(brew|cask|tap)\s+"([^"]+)"`)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := lineRe.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		kind := match[1]
		name := strings.TrimSpace(match[2])
		if kind == "tap" {
			info.Explicit[normalizeName("tap", name)] = true
			continue
		}
		if tap := tapNameFromQualifiedPackage(name); tap != "" {
			info.Implicit[tap] = true
		}
	}
	return info, scanner.Err()
}

func tapNameFromQualifiedPackage(name string) string {
	parts := strings.Split(strings.TrimSpace(name), "/")
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

func (p Provider) desiredFormulae(ctx context.Context) map[string]bool {
	items, err := p.Desired(ctx)
	if err != nil {
		return nil
	}
	formulae := map[string]bool{}
	for _, item := range items {
		if item.Kind == "brew" {
			formulae[normalizeName("brew", item.Name)] = true
		}
	}
	return formulae
}

func (p Provider) installedOnRequestFormulae(ctx context.Context, installed map[string]bool) (map[string]bool, bool) {
	if len(installed) == 0 {
		return nil, false
	}
	result := p.Runner.Run(ctx, "brew", "--cellar")
	if result.Err != nil || strings.TrimSpace(result.Stdout) == "" {
		return nil, false
	}
	cellar := strings.TrimSpace(result.Stdout)
	formulae := map[string]bool{}
	checked := 0
	for name := range installed {
		receipt, ok := installedFormulaReceipt(cellar, name)
		if !ok {
			continue
		}
		if receipt.InstalledOnRequest == nil {
			continue
		}
		checked++
		if *receipt.InstalledOnRequest {
			formulae[name] = true
		}
	}
	return formulae, checked > 0
}

type installReceipt struct {
	InstalledOnRequest *bool `json:"installed_on_request"`
}

func installedFormulaReceipt(cellar string, name string) (installReceipt, bool) {
	matches, err := filepath.Glob(filepath.Join(cellar, name, "*", "INSTALL_RECEIPT.json"))
	if err != nil || len(matches) == 0 {
		return installReceipt{}, false
	}
	for index := len(matches) - 1; index >= 0; index-- {
		data, err := os.ReadFile(matches[index])
		if err != nil {
			continue
		}
		var receipt installReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			continue
		}
		return receipt, true
	}
	return installReceipt{}, false
}

func brewCategoryFromComment(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return ""
	}
	switch {
	case strings.Contains(trimmed, "仕事用"), strings.Contains(trimmed, "work"):
		return "work"
	case strings.Contains(trimmed, "個人用"), strings.Contains(trimmed, "personal"):
		return "personal"
	case strings.Contains(trimmed, "work/personal"), strings.Contains(trimmed, "共通"):
		return "work"
	case strings.Contains(trimmed, "Linux"), strings.Contains(trimmed, "linux"):
		return "linux"
	default:
		return ""
	}
}

func trailingComment(line string) string {
	inQuote := false
	for i, r := range line {
		switch r {
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote {
				return strings.TrimSpace(line[i+1:])
			}
		}
	}
	return ""
}

func normalizeName(kind string, name string) string {
	name = strings.TrimSpace(name)
	if (kind == "brew" || kind == "cask") && strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		return parts[len(parts)-1]
	}
	return name
}

func splitLines(text string) []string {
	lines := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
