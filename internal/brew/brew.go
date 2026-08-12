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
	"sync"

	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

type Provider struct {
	Root              string
	Runner            runner.Runner
	IncludeVSCode     bool
	UseHomeDesired    bool
	AdditionalDesired []plan.Item
	DesiredResolver   *BootstrapDesiredResolver
}

type BootstrapDesiredResolver struct {
	root   string
	runner runner.Runner
	once   sync.Once
	items  []plan.Item
	err    error
}

func NewBootstrapDesiredResolver(root string, commandRunner runner.Runner) *BootstrapDesiredResolver {
	return &BootstrapDesiredResolver{root: root, runner: commandRunner}
}

func (resolver *BootstrapDesiredResolver) Resolve(ctx context.Context) ([]plan.Item, error) {
	resolver.once.Do(func() {
		packageSet, taps, err := mise.ReadBootstrapDesiredState(ctx, resolver.runner, resolver.root)
		if err != nil {
			resolver.err = err
			return
		}
		resolver.items = DesiredFromBootstrapPackages(packageSet, taps)
	})
	return append([]plan.Item(nil), resolver.items...), resolver.err
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
	additionalDesired := p.AdditionalDesired
	if p.DesiredResolver != nil {
		resolved, err := p.DesiredResolver.Resolve(ctx)
		if err != nil {
			return nil, err
		}
		additionalDesired = MergeDesired(additionalDesired, resolved)
	}
	path := p.desiredPath()
	items, err := DesiredFromPath(path)
	if err != nil {
		return nil, err
	}
	items = MergeDesired(items, additionalDesired)
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

const MiseBootstrapDesiredDetail = "desired source: mise bootstrap packages"

func DesiredFromBootstrapPackages(packageSet mise.BootstrapPackageSet, taps []mise.BootstrapTapDesired) []plan.Item {
	items := []plan.Item{}
	for _, pkg := range packageSet.Packages {
		kind := ""
		switch pkg.Manager {
		case "brew":
			kind = "brew"
		case "brew-cask":
			kind = "cask"
		default:
			continue
		}
		name := normalizeName(kind, pkg.Name)
		if name == "" {
			continue
		}
		items = append(items, plan.Item{Provider: "brew", Kind: kind, Name: name, Detail: MiseBootstrapDesiredDetail})
		if tap := tapNameFromQualifiedPackage(pkg.Name); tap != "" {
			items = append(items, plan.Item{Provider: "brew", Kind: "tap", Name: tap, Detail: MiseBootstrapDesiredDetail})
		}
	}
	for _, tap := range taps {
		name := normalizeName("tap", tap.Name)
		if name != "" {
			items = append(items, plan.Item{Provider: "brew", Kind: "tap", Name: name, Detail: MiseBootstrapDesiredDetail})
		}
	}
	return MergeDesired(nil, items)
}

func MergeDesired(groups ...[]plan.Item) []plan.Item {
	byKey := map[string]plan.Item{}
	for _, group := range groups {
		for _, item := range group {
			key := item.Kind + "\x00" + item.Name
			if existing, ok := byKey[key]; ok {
				if existing.Detail == "" {
					existing.Detail = item.Detail
				}
				byKey[key] = existing
				continue
			}
			byKey[key] = item
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]plan.Item, 0, len(keys))
	for _, key := range keys {
		items = append(items, byKey[key])
	}
	return items
}

func IsMiseBootstrapDesired(item plan.Item) bool {
	return item.Detail == MiseBootstrapDesiredDetail
}

func (p Provider) desiredPath() string {
	sourcePath := updevpath.RootBrewfileTemplate(p.Root)
	if !p.UseHomeDesired {
		return sourcePath
	}
	return updevpath.HomeOrRootBrewfile(p.Root)
}

func DesiredFromPath(path string) ([]plan.Item, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	lineRe := regexp.MustCompile(`^\s*(brew|cask|tap|vscode)\s+"([^"]+)"`)
	items := []plan.Item{}
	explicitTaps := map[string]bool{}
	implicitTaps := map[string]string{}
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
		rawName := strings.TrimSpace(match[2])
		name := normalizeName(kind, rawName)
		if kind == "tap" {
			explicitTaps[name] = true
		} else if tap := tapNameFromQualifiedPackage(rawName); tap != "" {
			if _, ok := implicitTaps[tap]; !ok {
				implicitTaps[tap] = category
			}
		}
		items = append(items, plan.Item{Provider: "brew", Kind: kind, Name: name, Category: category, Detail: trailingComment(line)})
	}
	implicitTapNames := make([]string, 0, len(implicitTaps))
	for tap := range implicitTaps {
		implicitTapNames = append(implicitTapNames, tap)
	}
	sort.Strings(implicitTapNames)
	for _, tap := range implicitTapNames {
		if explicitTaps[tap] {
			continue
		}
		category := implicitTaps[tap]
		items = append(items, plan.Item{
			Provider: "brew",
			Kind:     "tap",
			Name:     tap,
			Category: category,
			Detail:   "implicit tap from qualified brew/cask entry",
		})
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

	desiredItems, _ := p.Desired(ctx)
	desiredFormulae := desiredFormulaeFromItems(desiredItems)
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

	desiredTaps := desiredTapsFromItems(desiredItems)
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
			if query.kind == "tap" && isHomebrewDefaultTap(name) && !desiredTaps[normalizeName("tap", name)] {
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

func tapNameFromQualifiedPackage(name string) string {
	parts := strings.Split(strings.TrimSpace(name), "/")
	if len(parts) < 3 || parts[0] == "" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// TapNameFromQualifiedPackage returns the Homebrew tap portion of a qualified
// formula or cask name such as owner/repository/package.
func TapNameFromQualifiedPackage(name string) string {
	return tapNameFromQualifiedPackage(name)
}

func isHomebrewDefaultTap(name string) bool {
	switch normalizeName("tap", name) {
	case "homebrew/core", "homebrew/cask":
		return true
	default:
		return false
	}
}

func desiredFormulaeFromItems(items []plan.Item) map[string]bool {
	formulae := map[string]bool{}
	for _, item := range items {
		if item.Kind == "brew" {
			formulae[normalizeName("brew", item.Name)] = true
		}
	}
	return formulae
}

func desiredTapsFromItems(items []plan.Item) map[string]bool {
	taps := map[string]bool{}
	for _, item := range items {
		if item.Kind == "tap" {
			taps[normalizeName("tap", item.Name)] = true
		}
	}
	return taps
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

// NormalizeDesiredName applies the same identity normalization used by the
// Homebrew desired/live inventory adapters.
func NormalizeDesiredName(kind string, name string) string {
	return normalizeName(kind, name)
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
