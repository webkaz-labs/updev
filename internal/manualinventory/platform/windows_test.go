package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsWingetExportPathsUsesFixtureRootsForNonDefaultRoot(t *testing.T) {
	root := t.TempDir()
	got := WindowsWingetExportPaths(root, "/real/root")
	if len(got) != 2 || got[0] != filepath.Join(root, "winget-export.json") {
		t.Fatalf("expected fixture winget export paths, got %#v", got)
	}
}

func TestScanWindowsApplicationsReadsWingetExportFixture(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "winget-export.json")
	if err := os.WriteFile(path, []byte(`{
  "Sources": [
    {
      "Packages": [
        {"PackageIdentifier": "Microsoft.VisualStudioCode", "Version": "1.100.0"}
      ]
    }
  ]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	apps := ScanWindowsApplications(root, "/real/root")
	if len(apps) != 1 {
		t.Fatalf("expected one winget app, got %#v", apps)
	}
	app := apps[0]
	if app.Name != "VisualStudioCode" || app.Source != "winget export" || app.IdentifierKey != "package_id" || app.Identifier != "Microsoft.VisualStudioCode" || app.Version != "1.100.0" {
		t.Fatalf("unexpected winget app: %#v", app)
	}
}
