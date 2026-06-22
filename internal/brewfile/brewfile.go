package brewfile

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/webkaz-labs/updev/internal/updevpath"
)

var (
	lineRe           = regexp.MustCompile(`^\s*(brew|cask|tap|vscode)\s+"([^"]+)"`)
	categoryMarkerRe = regexp.MustCompile(`^\s*#\s*updev:\s*category\s+([A-Za-z0-9_-]+)\s*$`)
)

func Run(ctx context.Context, root string, args []string) int {
	if len(args) == 0 {
		usage()
		return 1
	}
	command := args[0]
	args = args[1:]
	switch command {
	case "add":
		if len(args) != 3 {
			usage()
			return 1
		}
		return runAdd(root, args[0], args[1], args[2])
	case "remove":
		if len(args) != 2 {
			usage()
			return 1
		}
		return runRemove(root, args[0], args[1])
	case "has":
		if len(args) != 2 {
			usage()
			return 1
		}
		if has(root, args[0], args[1]) {
			return 0
		}
		return 1
	case "check":
		mode := "--strict"
		if len(args) > 0 {
			mode = args[0]
		}
		return runCommand(ctx, "brewtmplcheck", mode)
	case "sync":
		mode := "--strict"
		if len(args) > 0 {
			mode = args[0]
		}
		_ = SyncHome(ctx)
		return runCommand(ctx, "brewtmplcheck", mode)
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		usage()
		return 1
	}
}

func SyncHome(ctx context.Context) error {
	home := updevpath.HomeDir()
	if home == "" {
		return fmt.Errorf("home directory unavailable")
	}
	return runCommandQuiet(ctx, "chezmoi", "apply", filepath.Join(home, "Brewfile"))
}

func runAdd(root string, kind string, name string, category string) int {
	changed, err := AddEntry(root, kind, name, category)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if !changed {
		fmt.Printf("already exists: %s %q\n", kind, name)
		return 0
	}
	line := fmt.Sprintf("%s %q", kind, name)
	fmt.Printf("added: %s -> %s\n", line, category)
	return 0
}

func runRemove(root string, kind string, name string) int {
	if _, err := RemoveEntry(root, kind, name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("removed: %s %q\n", kind, name)
	return 0
}

func AddEntry(root string, kind string, name string, category string) (bool, error) {
	if err := validateKind(kind); err != nil {
		return false, err
	}
	if category != "" {
		if err := validateCategoryName(category); err != nil {
			return false, err
		}
	}
	path := SourcePath(root)
	if HasEntry(root, kind, name) {
		return false, nil
	}
	text, mode, err := readSource(path)
	if err != nil {
		return false, fmt.Errorf("Brewfile.tmpl not found: %s", path)
	}
	line := fmt.Sprintf("%s %q", kind, name)
	updated, ok := insertIntoCategory(string(text), category, line)
	if !ok {
		if category == "" {
			return false, fmt.Errorf("Brewfile append target was not found")
		}
		return false, fmt.Errorf("%s block was not found", category)
	}
	if err := os.WriteFile(path, []byte(updated), mode); err != nil {
		return false, err
	}
	return true, nil
}

func Categories(root string) []string {
	text, _, err := readSource(SourcePath(root))
	if err != nil {
		return nil
	}
	return CategoriesFromTemplate(string(text))
}

func CategoriesFromTemplate(text string) []string {
	re := regexp.MustCompile(`has\s+"([^"]+)"\s+\.profiles`)
	seen := map[string]bool{}
	categories := []string{}
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if marker := categoryMarker(line); marker != "" {
			if !seen[marker] {
				seen[marker] = true
				categories = append(categories, marker)
			}
			continue
		}
		if isTemplateIf(line) && strings.Contains(line, `.profiles`) {
			for _, match := range re.FindAllStringSubmatch(line, -1) {
				category := strings.TrimSpace(match[1])
				if validateCategoryName(category) != nil || seen[category] {
					continue
				}
				seen[category] = true
				categories = append(categories, category)
			}
		}
	}
	return categories
}

func categoryMarker(line string) string {
	match := categoryMarkerRe.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	category := strings.TrimSpace(match[1])
	if validateCategoryName(category) != nil {
		return ""
	}
	return category
}

func hasCategoryBlocks(text string) bool {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := scanner.Text()
		if categoryMarker(line) != "" {
			return true
		}
		if isTemplateIf(line) && strings.Contains(line, `.profiles`) {
			if len(templateLineCategories(line)) > 0 {
				return true
			}
		}
	}
	return false
}

func appendUngrouped(text string, line string) string {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return line + "\n"
	}
	return trimmed + "\n" + line + "\n"
}

func insertIntoMarkerCategory(text string, category string, line string) (string, bool) {
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	inCategory := false
	inserted := false
	found := false
	for _, current := range lines {
		if marker := categoryMarker(current); marker != "" {
			if inCategory && !inserted {
				out.WriteString(line)
				out.WriteByte('\n')
				inserted = true
				inCategory = false
			}
			if marker == category {
				inCategory = true
				found = true
			}
			out.WriteString(current)
			continue
		}
		out.WriteString(current)
	}
	if inCategory && !inserted {
		out.WriteString(line)
		out.WriteByte('\n')
		inserted = true
	}
	return out.String(), found && inserted
}

func insertIntoTemplateCategory(text string, category string, line string) (string, bool) {
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	inCategory := false
	inserted := false
	found := false
	templateDepth := 0
	for _, current := range lines {
		if !inCategory && isCategoryTemplateStart(current, category) {
			inCategory = true
			found = true
			templateDepth = 1
			out.WriteString(current)
			continue
		}
		if inCategory {
			if isTemplateIf(current) {
				templateDepth++
			}
			if isTemplateEnd(current) {
				if templateDepth == 1 && !inserted {
					out.WriteString(line)
					out.WriteByte('\n')
					inserted = true
					inCategory = false
				}
				templateDepth--
			}
		}
		out.WriteString(current)
	}
	return out.String(), found && inserted
}

func templateLineCategories(line string) []string {
	re := regexp.MustCompile(`has\s+"([^"]+)"\s+\.profiles`)
	out := []string{}
	seen := map[string]bool{}
	for _, match := range re.FindAllStringSubmatch(line, -1) {
		category := strings.TrimSpace(match[1])
		if validateCategoryName(category) != nil || seen[category] {
			continue
		}
		seen[category] = true
		out = append(out, category)
	}
	return out
}

func RemoveEntry(root string, kind string, name string) (bool, error) {
	if err := validateKind(kind); err != nil {
		return false, err
	}
	path := SourcePath(root)
	text, mode, err := readSource(path)
	if err != nil {
		return false, fmt.Errorf("Brewfile.tmpl not found: %s", path)
	}
	updated := removeLine(string(text), kind, name)
	if updated == string(text) {
		return false, nil
	}
	if err := os.WriteFile(path, []byte(updated), mode); err != nil {
		return false, err
	}
	return true, nil
}

func HasEntry(root string, kind string, name string) bool {
	return has(root, kind, name)
}

func SourcePath(root string) string {
	return sourcePath(root)
}

func SourceExists(root string) bool {
	_, err := os.Stat(SourcePath(root))
	return err == nil
}

func has(root string, kind string, name string) bool {
	if err := validateKind(kind); err != nil {
		return false
	}
	file, err := os.Open(sourcePath(root))
	if err != nil {
		return false
	}
	defer file.Close()
	target := normalizeName(kind, name)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		match := lineRe.FindStringSubmatch(scanner.Text())
		if match == nil || match[1] != kind {
			continue
		}
		if normalizeName(kind, match[2]) == target {
			return true
		}
	}
	return false
}

func readSource(path string) ([]byte, os.FileMode, error) {
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

func insertIntoCategory(text string, category string, line string) (string, bool) {
	if strings.TrimSpace(category) == "" {
		if hasCategoryBlocks(text) {
			return text, false
		}
		return appendUngrouped(text, line), true
	}
	if updated, ok := insertIntoMarkerCategory(text, category, line); ok {
		return updated, true
	}
	return insertIntoTemplateCategory(text, category, line)
}

func isCategoryTemplateStart(line string, category string) bool {
	if !isTemplateIf(line) || !strings.Contains(line, `.profiles`) {
		return false
	}
	if category == "" {
		return false
	}
	for _, candidate := range templateLineCategories(line) {
		if candidate == category {
			return true
		}
	}
	return false
}

func isTemplateIf(line string) bool {
	return strings.Contains(line, "{{") && strings.Contains(line, "if ") && strings.Contains(line, "}}")
}

func isTemplateEnd(line string) bool {
	return strings.Contains(line, "{{") && strings.Contains(line, "end") && strings.Contains(line, "}}")
}

func removeLine(text string, kind string, name string) string {
	target := normalizeName(kind, name)
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	for _, line := range lines {
		match := lineRe.FindStringSubmatch(line)
		if match != nil && match[1] == kind && normalizeName(kind, match[2]) == target {
			continue
		}
		out.WriteString(line)
	}
	return out.String()
}

func validateKind(kind string) error {
	switch kind {
	case "brew", "cask", "tap", "vscode":
		return nil
	default:
		return fmt.Errorf("unsupported kind: %s", kind)
	}
}

func validateCategoryName(category string) error {
	category = strings.TrimSpace(category)
	if category == "" {
		return fmt.Errorf("unsupported category: %s", category)
	}
	for _, r := range category {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return fmt.Errorf("unsupported category: %s", category)
	}
	return nil
}

func normalizeName(kind string, name string) string {
	name = strings.TrimSpace(name)
	if (kind == "brew" || kind == "cask") && strings.Contains(name, "/") {
		parts := strings.Split(name, "/")
		return parts[len(parts)-1]
	}
	return name
}

func sourcePath(root string) string {
	if root == "" {
		root = updevpath.DefaultChezmoiSourceRoot()
	}
	return updevpath.RootBrewfileTemplate(root)
}

func runCommand(ctx context.Context, name string, args ...string) int {
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runCommandQuiet(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s exited with code %d", name, exit.ExitCode())
		}
		return err
	}
	return nil
}

func usage() {
	fmt.Fprintln(os.Stderr, `Usage:
  updev brewfile add <brew|cask|tap|vscode> <name> <category>
  updev brewfile remove <brew|cask|tap|vscode> <name>
  updev brewfile has <brew|cask|tap|vscode> <name>
  updev brewfile check [--strict|--lenient]
  updev brewfile sync [--strict|--lenient]`)
}
