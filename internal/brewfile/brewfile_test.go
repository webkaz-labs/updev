package brewfile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/runner"
)

func TestAddHasRemoveBrewfileEntry(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Brewfile.tmpl")
	content := `{{ if has "personal" .profiles }}
brew "git"
{{ end }}
{{ if has "work" .profiles }}
{{ end }}
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runAdd(root, "brew", "webkaz/tap/cmux-intel", "personal"); code != 0 {
		t.Fatalf("add failed with %d", code)
	}
	if !has(root, "brew", "cmux-intel") {
		t.Fatal("expected normalized brew entry to exist")
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), `brew "webkaz/tap/cmux-intel"`) {
		t.Fatalf("entry was not inserted: %s", updated)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected mode 0600 to be preserved, got %o", mode)
	}
	if code := runRemove(root, "brew", "cmux-intel"); code != 0 {
		t.Fatalf("remove failed with %d", code)
	}
	if has(root, "brew", "cmux-intel") {
		t.Fatal("expected entry to be removed")
	}
}

func TestAddRejectsUnknownCategory(t *testing.T) {
	if code := runAdd(t.TempDir(), "brew", "git", "common"); code == 0 {
		t.Fatal("expected unsupported category to fail")
	}
}

func TestInsertIntoCategoryUsesMatchingTemplateEnd(t *testing.T) {
	content := `{{- if has "personal" .profiles }}
{{- if eq .chezmoi.arch "amd64" }}
cask "intel-only"
{{- else }}
cask "arm-only"
{{- end }}
cask "always"
{{- end }}
`
	updated, ok := insertIntoCategory(content, "personal", `brew "new-tool"`)
	if !ok {
		t.Fatal("expected insert to succeed")
	}
	insertedAt := strings.Index(updated, `brew "new-tool"`)
	alwaysAt := strings.Index(updated, `cask "always"`)
	outerEndAt := strings.LastIndex(updated, "{{- end }}")
	if insertedAt < alwaysAt || insertedAt > outerEndAt {
		t.Fatalf("entry was not inserted before the outer category end:\n%s", updated)
	}
}

func TestInsertIntoWorkCategorySupportsBaselineProfileBlock(t *testing.T) {
	content := `{{- if or (has "work" .profiles) (has "personal" .profiles) }}
cask "cursor"
{{- end }}

{{- if has "personal" .profiles }}
cask "warp"
{{- end }}
`
	updated, ok := insertIntoCategory(content, "work", `cask "cursor-cli"`)
	if !ok {
		t.Fatal("expected insert to succeed")
	}
	insertedAt := strings.Index(updated, `cask "cursor-cli"`)
	workEndAt := strings.Index(updated, "{{- end }}")
	personalAt := strings.Index(updated, `{{- if has "personal" .profiles }}`)
	if insertedAt < 0 || insertedAt > workEndAt || insertedAt > personalAt {
		t.Fatalf("work entry was not inserted inside the baseline block:\n%s", updated)
	}
}

func TestInsertIntoCategoryMatchesCompoundProfileConditionWithoutRepoSpecificText(t *testing.T) {
	content := `{{- if and (has "runtime" .profiles) (ne .chezmoi.os "windows") }}
brew "go"
{{- end }}
`
	updated, ok := insertIntoCategory(content, "runtime", `brew "rust"`)
	if !ok {
		t.Fatal("expected insert to succeed")
	}
	if !strings.Contains(updated, `brew "rust"`) {
		t.Fatalf("runtime entry was not inserted:\n%s", updated)
	}
}

func TestCategoriesFromTemplateDetectsGenericUpdevMarkers(t *testing.T) {
	content := `# updev: category shared
brew "git"

# updev: category gui
cask "firefox"
`
	categories := CategoriesFromTemplate(content)
	if strings.Join(categories, ",") != "shared,gui" {
		t.Fatalf("unexpected categories: %#v", categories)
	}
}

func TestInsertIntoCategorySupportsGenericUpdevMarkers(t *testing.T) {
	content := `# updev: category shared
brew "git"

# updev: category gui
cask "firefox"
`
	updated, ok := insertIntoCategory(content, "shared", `brew "jq"`)
	if !ok {
		t.Fatal("expected marker insert to succeed")
	}
	jqAt := strings.Index(updated, `brew "jq"`)
	guiAt := strings.Index(updated, `# updev: category gui`)
	if jqAt < 0 || jqAt > guiAt {
		t.Fatalf("entry was not inserted before next category marker:\n%s", updated)
	}
}

func TestInsertIntoCategoryAppendsWhenBrewfileHasNoCategories(t *testing.T) {
	content := `brew "git"
`
	updated, ok := insertIntoCategory(content, "", `brew "jq"`)
	if !ok {
		t.Fatal("expected ungrouped insert to succeed")
	}
	if !strings.Contains(updated, "brew \"git\"\nbrew \"jq\"\n") {
		t.Fatalf("entry was not appended:\n%s", updated)
	}
}

func TestInsertIntoCategoryRejectsUngroupedInsertWhenCategoriesExist(t *testing.T) {
	content := `# updev: category shared
brew "git"
`
	if _, ok := insertIntoCategory(content, "", `brew "jq"`); ok {
		t.Fatal("expected ungrouped insert to fail when categories exist")
	}
}

func TestDesiredSourceDistinguishesBrewfileMiseBothAndNone(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte("brew \"git\"\nbrew \"jq\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &desiredSourceRunner{results: map[string]runner.Result{
		strings.Join([]string{"mise", "config", "get", "bootstrap", "--cd", root}, "\x00"): {Stdout: "[packages]\n\"brew:btop\" = \"latest\"\n\"brew:jq\" = \"latest\"\n"},
	}}
	single, err := DesiredSources(context.Background(), root, []string{"brew", "btop"}, fake)
	if err != nil || len(single) != 1 || single[0] != "mise" {
		t.Fatalf("single desired source = %v, err=%v", single, err)
	}
	fake.calls = nil
	got, err := DesiredSources(context.Background(), root, []string{"brew", "git", "brew", "btop", "brew", "jq", "brew", "curl"}, fake)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "brewfile,mise,both,none" {
		t.Fatalf("batched desired sources = %v", got)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("batched desired source must read resolved mise config once, calls=%v", fake.calls)
	}
}

type desiredSourceRunner struct {
	results map[string]runner.Result
	calls   [][]string
}

func (*desiredSourceRunner) LookPath(name string) (string, error) { return "/fake/" + name, nil }

func (fake *desiredSourceRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	call := append([]string{name}, args...)
	fake.calls = append(fake.calls, call)
	if result, ok := fake.results[strings.Join(call, "\x00")]; ok {
		return result
	}
	return runner.Result{Err: os.ErrNotExist, Code: 1}
}
