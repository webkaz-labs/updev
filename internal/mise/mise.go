package mise

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

type Provider struct {
	Root             string
	Runner           runner.Runner
	UseNativeDesired bool
}

type ManifestIssue struct {
	Path    string
	Line    int
	Tool    string
	Backend string
	Version string
	Reason  string
}

type ConfigSource struct {
	Path          string   `json:"path"`
	Environment   string   `json:"environment,omitempty"`
	ReportedOrder int      `json:"reported_order"`
	Tools         []string `json:"tools"`
}

func (Provider) Name() string { return "mise" }

func (p Provider) Supported(ctx context.Context) bool {
	_, err := p.Runner.LookPath("mise")
	return err == nil
}

func (p Provider) Desired(ctx context.Context) ([]plan.Item, error) {
	if !p.UseNativeDesired || p.Runner == nil {
		return desiredFromManifests(p.Root, p.Name())
	}
	return desiredFromMise(ctx, p.Root, p.Runner, p.Name())
}

func desiredFromMise(ctx context.Context, root string, commandRunner runner.Runner, providerName string) ([]plan.Item, error) {
	dirs, err := ManifestDirs(root)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	items := []plan.Item{}
	for _, dir := range dirs {
		current, err := currentTools(ctx, commandRunner, dir)
		if err != nil {
			return nil, err
		}
		for name := range current {
			key := "tool\x00" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, plan.Item{Provider: providerName, Kind: "tool", Name: name, Category: toolCategory(name)})
		}
	}
	return items, nil
}

func desiredFromManifests(root string, providerName string) ([]plan.Item, error) {
	if _, err := os.Stat(ConfigPath(root)); err != nil {
		return nil, err
	}
	paths, err := ManifestPaths(root)
	if err != nil {
		return nil, err
	}
	items := []plan.Item{}
	seen := map[string]bool{}
	for _, path := range paths {
		pathItems, err := desiredFromPath(providerName, path)
		if err != nil {
			return nil, err
		}
		for _, item := range pathItems {
			key := item.Kind + "\x00" + item.Name
			if seen[key] {
				continue
			}
			seen[key] = true
			items = append(items, item)
		}
	}
	return items, nil
}

func desiredFromPath(providerName string, path string) ([]plan.Item, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	items := []plan.Item{}
	inTools := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := stripComment(scanner.Text())
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inTools = trimmed == "[tools]"
			continue
		}
		if !inTools {
			continue
		}
		if !matchesCurrentOS(trimmed) {
			continue
		}
		name, ok := parseToolName(trimmed)
		if !ok {
			continue
		}
		items = append(items, plan.Item{Provider: providerName, Kind: "tool", Name: name, Category: toolCategory(name)})
	}
	return items, scanner.Err()
}

func ConfigPath(root string) string {
	return filepath.Join(root, "dot_config", "mise", "config.toml")
}

func ConfigSources(ctx context.Context, commandRunner runner.Runner, root string) ([]ConfigSource, error) {
	result := commandRunner.Run(ctx, "mise", "config", "ls", "--json", "--cd", root)
	if result.Err != nil {
		return nil, fmt.Errorf("mise config ls --json: %s", runner.ResultDetail(result, "command failed", runner.ResultDetailOption{}))
	}
	return ConfigSourcesFromJSON([]byte(result.Stdout))
}

func ConfigSourcesFromJSON(data []byte) ([]ConfigSource, error) {
	var sources []ConfigSource
	if err := json.Unmarshal(data, &sources); err != nil {
		return nil, err
	}
	normalized := make([]ConfigSource, 0, len(sources))
	seenPaths := map[string]bool{}
	for _, source := range sources {
		path := strings.TrimSpace(source.Path)
		if path == "" {
			if len(source.Tools) > 0 {
				return nil, fmt.Errorf("mise config source path is empty")
			}
			continue
		}
		path = filepath.Clean(path)
		if seenPaths[path] {
			return nil, fmt.Errorf("duplicate mise config source path: %s", path)
		}
		seenPaths[path] = true

		seenTools := map[string]bool{}
		tools := make([]string, 0, len(source.Tools))
		for _, rawTool := range source.Tools {
			tool := strings.TrimSpace(rawTool)
			if tool == "" {
				return nil, fmt.Errorf("mise config source %s contains an empty tool name", path)
			}
			if seenTools[tool] {
				return nil, fmt.Errorf("mise config source %s contains duplicate tool %q", path, tool)
			}
			seenTools[tool] = true
			tools = append(tools, tool)
		}
		source.Path = path
		source.Environment = configEnvironment(path)
		source.ReportedOrder = len(normalized) + 1
		source.Tools = tools
		sort.Strings(source.Tools)
		normalized = append(normalized, source)
	}
	return normalized, nil
}

func configEnvironment(path string) string {
	name := filepath.Base(path)
	for _, prefix := range []string{"config.", "mise.", ".mise."} {
		if len(name) <= len(prefix)+len(".toml") || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".toml") {
			continue
		}
		environment := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".toml")
		if environment != "" {
			return environment
		}
	}
	return ""
}

func (source ConfigSource) Diagnostic() string {
	attributes := []string{fmt.Sprintf("reported_order=%d", source.ReportedOrder)}
	if source.Environment != "" {
		attributes = append(attributes, "environment="+source.Environment)
	}
	attributes = append(attributes, fmt.Sprintf("tools=%d", len(source.Tools)))
	return fmt.Sprintf("%s (%s)", source.Path, strings.Join(attributes, ", "))
}

func HasTool(root string, name string) bool {
	tools, err := DesiredTools(root)
	if err != nil {
		return false
	}
	_, ok := tools[strings.TrimSpace(name)]
	return ok
}

func DesiredTools(root string) (map[string]string, error) {
	paths, err := ManifestPaths(root)
	if err != nil {
		return nil, err
	}
	tools := map[string]string{}
	for _, path := range paths {
		pathTools, err := desiredToolsFromPath(path)
		if err != nil {
			return nil, err
		}
		for name, version := range pathTools {
			if _, ok := tools[name]; ok {
				continue
			}
			tools[name] = version
		}
	}
	return tools, nil
}

func desiredToolsFromPath(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	tools := map[string]string{}
	inTools := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := stripComment(scanner.Text())
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inTools = trimmed == "[tools]"
			continue
		}
		if !inTools {
			continue
		}
		name, ok := parseToolName(trimmed)
		if !ok {
			continue
		}
		tools[name] = strings.TrimSpace(trimmed[strings.Index(trimmed, "=")+1:])
	}
	return tools, scanner.Err()
}

func ManifestIssues(root string) ([]ManifestIssue, error) {
	paths, err := ManifestPaths(root)
	if err != nil {
		return nil, err
	}
	issues := []ManifestIssue{}
	for _, path := range paths {
		pathIssues, err := manifestIssuesFromPath(path)
		if err != nil {
			return nil, err
		}
		issues = append(issues, pathIssues...)
	}
	return issues, nil
}

func ManifestPaths(root string) ([]string, error) {
	seen := map[string]bool{}
	paths := []string{}
	add := func(path string) {
		cleaned := filepath.Clean(path)
		if seen[cleaned] {
			return
		}
		if _, err := os.Stat(cleaned); err != nil {
			return
		}
		seen[cleaned] = true
		paths = append(paths, cleaned)
	}
	add(ConfigPath(root))
	for _, path := range parentManifestPaths(root) {
		add(path)
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipMiseManifestDir(root, path) {
				return fs.SkipDir
			}
			return nil
		}
		switch entry.Name() {
		case "mise.toml", ".mise.toml":
			add(path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func ManifestDirs(root string) ([]string, error) {
	seen := map[string]bool{}
	dirs := []string{}
	add := func(dir string) {
		cleaned := filepath.Clean(dir)
		if seen[cleaned] {
			return
		}
		seen[cleaned] = true
		dirs = append(dirs, cleaned)
	}
	add(root)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipMiseManifestDir(root, path) {
				return fs.SkipDir
			}
			return nil
		}
		switch entry.Name() {
		case "mise.toml", ".mise.toml":
			add(filepath.Dir(path))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	return dirs, nil
}

func parentManifestPaths(root string) []string {
	paths := []string{}
	current := filepath.Clean(root)
	for {
		for _, name := range []string{"mise.toml", ".mise.toml"} {
			paths = append(paths, filepath.Join(current, name))
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return paths
}

func shouldSkipMiseManifestDir(root string, path string) bool {
	if filepath.Clean(path) == filepath.Clean(root) {
		return false
	}
	switch filepath.Base(path) {
	case ".git", ".jj", "node_modules", "vendor", ".cache", ".pytest_cache", ".mypy_cache":
		return true
	default:
		return false
	}
}

func manifestIssuesFromPath(path string) ([]ManifestIssue, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	issues := []ManifestIssue{}
	inTools := false
	lineNumber := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lineNumber++
		line := stripComment(scanner.Text())
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inTools = trimmed == "[tools]"
			continue
		}
		if !inTools {
			continue
		}
		name, rawValue, ok := parseToolAssignment(trimmed)
		if !ok {
			continue
		}
		version, hasVersion := parseToolVersion(rawValue)
		reason := miseVersionPinReason(name, version, hasVersion)
		if reason == "" {
			continue
		}
		issues = append(issues, ManifestIssue{
			Path:    path,
			Line:    lineNumber,
			Tool:    name,
			Backend: toolBackend(name),
			Version: version,
			Reason:  reason,
		})
	}
	return issues, scanner.Err()
}

func AddTool(root string, name string, version string) (bool, error) {
	name = strings.TrimSpace(name)
	version = strings.TrimSpace(version)
	if name == "" {
		return false, fmt.Errorf("mise tool name is required")
	}
	if version == "" {
		return false, fmt.Errorf("mise add requires an explicit version")
	}
	if reason := miseVersionPinReason(name, version, true); reason != "" {
		return false, fmt.Errorf("%s", reason)
	}
	path := ConfigPath(root)
	text, mode, err := readConfig(path)
	if err != nil {
		return false, err
	}
	if HasTool(root, name) {
		return false, nil
	}
	line := fmt.Sprintf("%s = %q", quoteToolName(name), version)
	updated, ok := insertToolLine(string(text), line)
	if !ok {
		return false, fmt.Errorf("[tools] section was not found")
	}
	if err := os.WriteFile(path, []byte(updated), mode); err != nil {
		return false, err
	}
	return true, nil
}

func RemoveTool(root string, name string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, fmt.Errorf("mise tool name is required")
	}
	path := ConfigPath(root)
	text, mode, err := readConfig(path)
	if err != nil {
		return false, err
	}
	updated := removeToolLine(string(text), name)
	if updated == string(text) {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(updated), mode); err != nil {
		return false, err
	}
	return true, nil
}

func RenameTool(root string, current string, recommended string) (bool, error) {
	current = strings.TrimSpace(current)
	recommended = strings.TrimSpace(recommended)
	if current == "" || recommended == "" {
		return false, fmt.Errorf("mise tool names are required")
	}
	if current == recommended {
		return false, nil
	}
	if HasTool(root, recommended) {
		return false, fmt.Errorf("recommended mise tool already exists: %s", recommended)
	}
	paths, err := ManifestPaths(root)
	if err != nil {
		return false, err
	}
	for _, path := range paths {
		text, mode, err := readConfig(path)
		if err != nil {
			return false, err
		}
		updated, changed := renameToolLine(string(text), current, recommended)
		if !changed {
			continue
		}
		if err := os.WriteFile(path, []byte(updated), mode); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func readConfig(path string) ([]byte, os.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	return text, info.Mode().Perm(), nil
}

func insertToolLine(text string, line string) (string, bool) {
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	inTools := false
	inserted := false
	found := false
	for _, current := range lines {
		trimmed := strings.TrimSpace(current)
		if trimmed == "[tools]" {
			inTools = true
			found = true
			out.WriteString(current)
			continue
		}
		if inTools && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if !inserted {
				out.WriteString(line)
				out.WriteByte('\n')
				inserted = true
			}
			inTools = false
		}
		out.WriteString(current)
	}
	if found && !inserted {
		if !strings.HasSuffix(out.String(), "\n") {
			out.WriteByte('\n')
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String(), found
}

func removeToolLine(text string, name string) string {
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	inTools := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(stripComment(line))
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inTools = trimmed == "[tools]"
			out.WriteString(line)
			continue
		}
		if inTools {
			toolName, ok := parseToolName(trimmed)
			if ok && toolName == name {
				continue
			}
		}
		out.WriteString(line)
	}
	return out.String()
}

func renameToolLine(text string, current string, recommended string) (string, bool) {
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	inTools := false
	changed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(stripComment(line))
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inTools = trimmed == "[tools]"
			out.WriteString(line)
			continue
		}
		if inTools && !changed {
			toolName, _, ok := parseToolAssignment(trimmed)
			if ok && toolName == current {
				indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
				eqIndex := strings.Index(line, "=")
				if eqIndex < 0 {
					out.WriteString(line)
					continue
				}
				out.WriteString(indent + quoteToolName(recommended) + " " + strings.TrimLeft(line[eqIndex:], " \t"))
				changed = true
				continue
			}
		}
		out.WriteString(line)
	}
	return out.String(), changed
}

func quoteToolName(name string) string {
	if strings.ContainsAny(name, ":/@.-") {
		return fmt.Sprintf("%q", name)
	}
	return name
}

func (p Provider) Live(ctx context.Context) ([]plan.Item, error) {
	current, err := currentTools(ctx, p.Runner, p.Root)
	if err != nil {
		return nil, err
	}
	items := make([]plan.Item, 0, len(current))
	seen := map[string]bool{}
	for name, states := range current {
		raw, err := json.Marshal(states)
		if err != nil || !miseToolInstalled(raw) {
			continue
		}
		seen[name] = true
		items = append(items, plan.Item{Provider: p.Name(), Kind: "tool", Name: name, Category: toolCategory(name)})
	}
	desired, err := p.Desired(ctx)
	if err != nil {
		return items, nil
	}
	for _, item := range desired {
		if seen[item.Name] {
			continue
		}
		if p.installedTool(ctx, item.Name) {
			seen[item.Name] = true
			items = append(items, plan.Item{Provider: p.Name(), Kind: "tool", Name: item.Name, Category: toolCategory(item.Name)})
		}
	}
	return items, nil
}

func currentTools(ctx context.Context, commandRunner runner.Runner, dir string) (map[string][]miseToolState, error) {
	result := commandRunner.Run(ctx, "mise", "ls", "--current", "--json", "--cd", dir)
	if result.Err != nil {
		return nil, result.Err
	}
	var current map[string][]miseToolState
	if err := json.Unmarshal([]byte(result.Stdout), &current); err != nil {
		return nil, err
	}
	return current, nil
}

func (p Provider) installedTool(ctx context.Context, name string) bool {
	result := p.Runner.Run(ctx, "mise", "ls", "--json", name)
	if result.Err != nil {
		return false
	}
	return miseToolInstalled(json.RawMessage(result.Stdout))
}

type miseToolState struct {
	Version          string `json:"version"`
	RequestedVersion string `json:"requested_version"`
	Installed        *bool  `json:"installed"`
}

func miseToolInstalled(raw json.RawMessage) bool {
	states := []miseToolState{}
	if err := json.Unmarshal(raw, &states); err == nil {
		for _, state := range states {
			if state.Installed == nil || *state.Installed {
				return true
			}
		}
		return false
	}
	var state miseToolState
	if err := json.Unmarshal(raw, &state); err == nil {
		return state.Installed == nil || *state.Installed
	}
	return false
}

func toolCategory(name string) string {
	switch {
	case name == "go" || name == "node" || name == "python" || name == "rust" || name == "uv" || name == "bun":
		return "runtime"
	case strings.HasPrefix(name, "npm:"):
		return "npm"
	case strings.HasPrefix(name, "cargo:"):
		return "cargo"
	case strings.HasPrefix(name, "github:"):
		return "github"
	case strings.HasPrefix(name, "pipx:"):
		return "pipx"
	case strings.HasPrefix(name, "aqua:"):
		return "aqua"
	case strings.HasPrefix(name, "vfox:"):
		return "vfox"
	default:
		return "core"
	}
}

func parseToolName(line string) (string, bool) {
	name, _, ok := parseToolAssignment(line)
	return name, ok
}

func parseToolAssignment(line string) (string, string, bool) {
	idx := strings.Index(line, "=")
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	key = strings.Trim(key, `"`)
	if key == "" {
		return "", "", false
	}
	return key, strings.TrimSpace(line[idx+1:]), true
}

func parseToolVersion(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if strings.HasPrefix(raw, "{") {
		for _, part := range strings.Split(strings.Trim(raw, "{}"), ",") {
			key, value, ok := strings.Cut(part, "=")
			if !ok || strings.TrimSpace(key) != "version" {
				continue
			}
			return strings.Trim(strings.TrimSpace(value), `"'`), true
		}
		return "", false
	}
	return strings.Trim(strings.TrimSpace(raw), `"'`), true
}

func miseVersionPinReason(name string, version string, hasVersion bool) string {
	if !hasVersion || strings.TrimSpace(version) == "" {
		return "mise tool entry has no version; pin an exact version"
	}
	normalized := strings.ToLower(strings.TrimSpace(version))
	if normalized == "latest" {
		return "latest is not allowed; pin an exact version"
	}
	if normalized == "lts" && name != "node" {
		return "lts is only allowed for node; pin an exact version"
	}
	return ""
}

func toolBackend(name string) string {
	if prefix, _, ok := strings.Cut(name, ":"); ok {
		return prefix
	}
	return "core"
}

func stripComment(line string) string {
	inQuote := false
	for i, r := range line {
		switch r {
		case '"':
			inQuote = !inQuote
		case '#':
			if !inQuote {
				return line[:i]
			}
		}
	}
	return line
}

func matchesCurrentOS(line string) bool {
	osIndex := strings.Index(line, "os")
	if osIndex < 0 {
		return true
	}
	assignment := line[osIndex:]
	if !strings.Contains(assignment, "=") || !strings.Contains(assignment, "[") {
		return true
	}
	start := strings.Index(assignment, "[")
	end := strings.Index(assignment[start:], "]")
	if start < 0 || end < 0 {
		return true
	}
	rawList := assignment[start+1 : start+end]
	current := currentOSTokens()
	for _, raw := range strings.Split(rawList, ",") {
		token := strings.Trim(strings.TrimSpace(raw), `"`)
		if current[token] {
			return true
		}
	}
	return false
}

func currentOSTokens() map[string]bool {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	archName := runtime.GOARCH
	if archName == "amd64" {
		archName = "x64"
	}
	return map[string]bool{
		osName:                  true,
		osName + "/" + archName: true,
		runtime.GOOS:            true,
	}
}
