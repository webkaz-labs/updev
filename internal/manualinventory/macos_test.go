package manualinventory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMacApplicationRootsUsesFixtureRootsForNonDefaultRoot(t *testing.T) {
	root := t.TempDir()
	got := MacApplicationRoots(root, "/real/root")
	want := []string{
		filepath.Join(root, "Applications"),
		filepath.Join(root, "home", "Applications"),
	}
	if len(got) != len(want) {
		t.Fatalf("expected roots %#v, got %#v", want, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected roots %#v, got %#v", want, got)
		}
	}
}

func TestScanMacApplicationRootsReadsBundlesAndReceipts(t *testing.T) {
	root := t.TempDir()
	appPath := filepath.Join(root, "Applications", "Demo.app")
	writeTestAppBundle(t, appPath, map[string]string{
		"CFBundleDisplayName":        "Demo",
		"CFBundleIdentifier":         "com.example.demo",
		"CFBundleShortVersionString": "1.2.3",
	})
	receipt := filepath.Join(appPath, "Contents", "_MASReceipt")
	if err := os.MkdirAll(receipt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(receipt, "receipt"), []byte("receipt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Applications", "NotApp"), 0o755); err != nil {
		t.Fatal(err)
	}

	apps := ScanMacApplicationRoots([]string{filepath.Join(root, "Applications")})
	if len(apps) != 1 {
		t.Fatalf("expected one app, got %#v", apps)
	}
	app := apps[0]
	if app.Name != "Demo" || app.BundleID != "com.example.demo" || app.Version != "1.2.3" {
		t.Fatalf("unexpected app metadata: %#v", app)
	}
	if app.Source != "mac app store receipt" {
		t.Fatalf("expected MAS receipt source, got %#v", app)
	}
	if app.Path != filepath.Clean(appPath) {
		t.Fatalf("expected clean app path %q, got %q", filepath.Clean(appPath), app.Path)
	}
}

func writeTestAppBundle(t *testing.T, path string, values map[string]string) {
	t.Helper()
	contents := filepath.Join(path, "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>`
	for key, value := range values {
		body += "<key>" + key + "</key><string>" + value + "</string>"
	}
	body += "</dict></plist>"
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
