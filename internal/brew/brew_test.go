package brew

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/runner"
)

func TestDesiredParsesBrewfileTemplate(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	content := `
brew "git"
cask "visual-studio-code"
tap "webkaz/tap"
vscode "ms-vscode.go"
brew "webkaz/tap/cmux-intel"
`
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := (Provider{Root: root, Runner: nil, IncludeVSCode: true}).Desired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, item := range items {
		got[item.Kind+":"+item.Name] = true
	}
	for _, want := range []string{"brew:git", "cask:visual-studio-code", "tap:webkaz/tap", "vscode:ms-vscode.go", "brew:cmux-intel"} {
		if !got[want] {
			t.Fatalf("missing %s in %#v", want, got)
		}
	}
}

func TestProviderDesiredExcludesVSCodeByDefault(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	content := `
brew "git"
vscode "ms-vscode.go"
`
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := (Provider{Root: root, Runner: nil}).Desired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Kind == "vscode" {
			t.Fatalf("vscode should be opt-in, got %#v", items)
		}
	}
}

func TestProviderDesiredUsesSourceRootUnlessHomeDesiredEnabled(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(`brew "source-tool"`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Brewfile"), []byte(`brew "home-tool"`), 0o600); err != nil {
		t.Fatal(err)
	}
	sourceItems, err := (Provider{Root: root, Runner: nil}).Desired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sourceItems) != 1 || sourceItems[0].Name != "source-tool" {
		t.Fatalf("expected source-root desired state, got %#v", sourceItems)
	}
	homeItems, err := (Provider{Root: root, Runner: nil, UseHomeDesired: true}).Desired(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(homeItems) != 1 || homeItems[0].Name != "home-tool" {
		t.Fatalf("expected home rendered desired state, got %#v", homeItems)
	}
}

func TestDesiredUsesWorkCategoryAsHomebrewDefault(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "Brewfile.tmpl")
	content := `
# work - baseline macOS Homebrew entries, also included by personal profile
brew "git"

# personal - private-use desired entries
cask "warp"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err := DesiredFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	categories := map[string]string{}
	for _, item := range items {
		categories[item.Name] = item.Category
	}
	if categories["git"] != "work" || categories["warp"] != "personal" {
		t.Fatalf("unexpected categories: %#v", categories)
	}
}

func TestLiveFormulaeUseRequestedAndDesiredInstalledFormulae(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cellar := filepath.Join(root, "Cellar")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Brewfile"), []byte(`
brew "git"
brew "desired-dependency"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeInstallReceipt(t, cellar, "git", true)
	writeInstallReceipt(t, cellar, "desired-dependency", false)
	writeInstallReceipt(t, cellar, "transient-dependency", false)
	writeInstallReceipt(t, cellar, "leaf-only", true)
	fake := fakeRunner{
		results: map[string]runner.Result{
			"brew\x00list\x00--formula\x00-1": {Stdout: "git\ndesired-dependency\ntransient-dependency\nleaf-only\n"},
			"brew\x00--cellar":                {Stdout: cellar},
			"brew\x00list\x00--cask\x00-1":    {Stdout: "visual-studio-code\n"},
			"brew\x00tap":                     {Stdout: "homebrew/core\n"},
			"code\x00--list-extensions":       {Stdout: "ms-vscode.go\n"},
		},
		paths: map[string]bool{"brew": true, "code": true},
	}
	items, err := (Provider{Root: root, Runner: fake, IncludeVSCode: true, UseHomeDesired: true}).Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, item := range items {
		got[item.Kind+":"+item.Name] = true
	}
	for _, want := range []string{
		"brew:git",
		"brew:desired-dependency",
		"brew:leaf-only",
		"cask:visual-studio-code",
		"tap:homebrew/core",
		"vscode:ms-vscode.go",
	} {
		if !got[want] {
			t.Fatalf("missing live item %s in %#v", want, got)
		}
	}
	if got["brew:transient-dependency"] {
		t.Fatalf("transient dependency should not be reported as live formula: %#v", got)
	}
}

func TestExplicitFormulaeUseInstallReceipts(t *testing.T) {
	root := t.TempDir()
	cellar := filepath.Join(root, "Cellar")
	writeInstallReceipt(t, cellar, "git", true)
	writeInstallReceipt(t, cellar, "transient-dependency", false)
	fake := fakeRunner{
		results: map[string]runner.Result{
			"brew\x00list\x00--formula\x00-1": {Stdout: "git\ntransient-dependency\n"},
			"brew\x00--cellar":                {Stdout: cellar},
			"brew\x00leaves":                  {Stdout: "git\ntransient-dependency\n"},
		},
		paths: map[string]bool{"brew": true},
	}
	names, err := (Provider{Root: root, Runner: fake}).ExplicitFormulae(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "git" {
		t.Fatalf("expected only receipt-requested formulae, got %#v", names)
	}
}

func TestLiveExcludesImplicitTapForQualifiedDesiredCask(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Brewfile"), []byte(`
cask "webkaz/tap/cmux-intel"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := fakeRunner{
		results: map[string]runner.Result{
			"brew\x00list\x00--formula\x00-1": {Stdout: ""},
			"brew\x00leaves":                  {Stdout: ""},
			"brew\x00list\x00--cask\x00-1":    {Stdout: "cmux-intel\n"},
			"brew\x00tap":                     {Stdout: "webkaz/tap\n"},
		},
		paths: map[string]bool{"brew": true},
	}
	items, err := (Provider{Root: root, Runner: fake, UseHomeDesired: true}).Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, item := range items {
		got[item.Kind+":"+item.Name] = true
	}
	if got["tap:webkaz/tap"] {
		t.Fatalf("implicit tap should not be reported as live drift: %#v", got)
	}
	if !got["cask:cmux-intel"] {
		t.Fatalf("expected qualified cask to stay live, got %#v", got)
	}
}

func TestLiveKeepsExplicitTapForQualifiedDesiredCask(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "Brewfile"), []byte(`
tap "webkaz/tap"
cask "webkaz/tap/cmux-intel"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := fakeRunner{
		results: map[string]runner.Result{
			"brew\x00list\x00--formula\x00-1": {Stdout: ""},
			"brew\x00leaves":                  {Stdout: ""},
			"brew\x00list\x00--cask\x00-1":    {Stdout: "cmux-intel\n"},
			"brew\x00tap":                     {Stdout: "webkaz/tap\n"},
		},
		paths: map[string]bool{"brew": true},
	}
	items, err := (Provider{Root: root, Runner: fake, UseHomeDesired: true}).Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, item := range items {
		got[item.Kind+":"+item.Name] = true
	}
	if !got["tap:webkaz/tap"] {
		t.Fatalf("explicit tap should remain live, got %#v", got)
	}
}

func TestLiveFormulaeFallbackToFormulaListWhenLeavesUnavailable(t *testing.T) {
	root := t.TempDir()
	fake := fakeRunner{
		results: map[string]runner.Result{
			"brew\x00list\x00--formula\x00-1": {Stdout: "git\ntransient-dependency\n"},
			"brew\x00leaves":                  {Err: os.ErrNotExist, Code: 1},
		},
		paths: map[string]bool{"brew": true},
	}
	items, err := (Provider{Root: root, Runner: fake}).Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, item := range items {
		got[item.Kind+":"+item.Name] = true
	}
	if !got["brew:git"] || !got["brew:transient-dependency"] {
		t.Fatalf("expected formula-list fallback, got %#v", got)
	}
}

func writeInstallReceipt(t *testing.T, cellar string, name string, installedOnRequest bool) {
	t.Helper()
	dir := filepath.Join(cellar, name, "1.0.0")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	value := "false"
	if installedOnRequest {
		value = "true"
	}
	if err := os.WriteFile(filepath.Join(dir, "INSTALL_RECEIPT.json"), []byte(`{"installed_on_request":`+value+`}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

type fakeRunner struct {
	results map[string]runner.Result
	paths   map[string]bool
}

func (fake fakeRunner) LookPath(name string) (string, error) {
	if fake.paths[name] {
		return "/fake/" + name, nil
	}
	return "", os.ErrNotExist
}

func (fake fakeRunner) Run(_ context.Context, name string, args ...string) runner.Result {
	if fake.results == nil {
		return runner.Result{}
	}
	if result, ok := fake.results[strings.Join(append([]string{name}, args...), "\x00")]; ok {
		return result
	}
	return runner.Result{Err: os.ErrNotExist, Code: 1}
}
