package securityscanner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolsSupportsExplicitFilters(t *testing.T) {
	root := t.TempDir()
	got := Tools("none", root)
	if len(got) != 0 {
		t.Fatalf("expected none scanner to disable scanners, got %#v", got)
	}
	got = Tools("osv,gitleaks,secrets,trivy-fs,anchore-grype", root)
	want := []string{"osv-scanner", "gitleaks", "trivy", "grype"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected scanner tools: %#v", got)
	}
	got = Tools("all", root)
	want = []string{"osv-scanner", "gitleaks", "zizmor", "trivy", "grype"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected all scanner tools: %#v", got)
	}
}

func TestToolsAutoIncludesZizmorOnlyWithWorkflowFiles(t *testing.T) {
	root := t.TempDir()
	got := Tools("auto", root)
	if strings.Join(got, ",") != "osv-scanner,gitleaks" {
		t.Fatalf("unexpected auto scanners without workflows: %#v", got)
	}
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workflowDir, "ci.yml"), []byte("name: ci\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = Tools("auto", root)
	if strings.Join(got, ",") != "osv-scanner,gitleaks,zizmor" {
		t.Fatalf("unexpected auto scanners with workflows: %#v", got)
	}
}

func TestParseNamesRejectsInvalidCombinations(t *testing.T) {
	if _, err := ParseNames("unknown"); err == nil {
		t.Fatal("expected unsupported scanner error")
	}
	if _, err := ParseNames("auto,gitleaks"); err == nil {
		t.Fatal("expected combined auto scanner error")
	}
}
