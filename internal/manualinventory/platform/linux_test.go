package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxApplicationRootsUsesFixtureRootsForNonDefaultRoot(t *testing.T) {
	root := t.TempDir()
	got := LinuxApplicationRoots(root, "/real/root")
	wantDesktop := filepath.Join(root, "usr", "share", "applications")
	if len(got.DesktopEntries) == 0 || got.DesktopEntries[0] != wantDesktop {
		t.Fatalf("expected fixture desktop root %q, got %#v", wantDesktop, got)
	}
	if len(got.FlatpakApps) == 0 || got.FlatpakApps[0] != filepath.Join(root, "var", "lib", "flatpak", "app") {
		t.Fatalf("expected fixture flatpak roots, got %#v", got)
	}
}

func TestScanLinuxApplicationsReadsDesktopFlatpakSnapAndAppImageEvidence(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "usr", "share", "applications", "org.example.Demo.desktop"), `[Desktop Entry]
Name=Demo
X-Flatpak=org.example.Demo
X-Version=1.2.3
`)
	if err := os.MkdirAll(filepath.Join(root, "var", "lib", "flatpak", "app", "org.example.FlatpakOnly"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "snap", "snapdemo"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "home", "Applications", "Tool.AppImage"), "")

	apps := ScanLinuxApplications(root, "/real/root")
	bySource := map[string]App{}
	for _, app := range apps {
		bySource[app.Source] = app
	}
	desktop := bySource["flatpak desktop entry"]
	if desktop.Name != "Demo" || desktop.IdentifierKey != "package_id" || desktop.Identifier != "org.example.Demo" || desktop.Version != "1.2.3" {
		t.Fatalf("unexpected desktop evidence: %#v", desktop)
	}
	if bySource["flatpak metadata"].Identifier != "org.example.FlatpakOnly" {
		t.Fatalf("expected flatpak metadata evidence, got %#v", apps)
	}
	if bySource["snap package"].Identifier != "snapdemo" {
		t.Fatalf("expected snap evidence, got %#v", apps)
	}
	if bySource["appimage file"].Name != "Tool" {
		t.Fatalf("expected AppImage evidence, got %#v", apps)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
