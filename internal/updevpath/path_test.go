package updevpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFileUsesEnvOverride(t *testing.T) {
	t.Setenv("UPDEV_CONFIG", "/tmp/updev.toml")
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))

	if got := ConfigFile(); got != "/tmp/updev.toml" {
		t.Fatalf("expected env override, got %q", got)
	}
}

func TestConfigFileUsesXDGConfigHome(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config")
	t.Setenv("UPDEV_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	want := filepath.Join(configHome, "updev", "config.toml")
	if got := ConfigFile(); got != want {
		t.Fatalf("expected XDG config path %q, got %q", want, got)
	}
}

func TestPackageMetadataFileUsesXDGConfigHome(t *testing.T) {
	configHome := filepath.Join(t.TempDir(), "config")
	t.Setenv("XDG_CONFIG_HOME", configHome)

	want := filepath.Join(configHome, "updev", "package-metadata.toml")
	if got := PackageMetadataFile(); got != want {
		t.Fatalf("expected package metadata path %q, got %q", want, got)
	}
}

func TestCacheDirUsesXDGCacheHome(t *testing.T) {
	cacheHome := filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	want := filepath.Join(cacheHome, "updev")
	if got := CacheDir(); got != want {
		t.Fatalf("expected XDG cache dir %q, got %q", want, got)
	}
}

func TestReportCacheDirUsesCacheSubdir(t *testing.T) {
	cacheHome := filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	want := filepath.Join(cacheHome, "updev", "reports")
	if got := ReportCacheDir(); got != want {
		t.Fatalf("expected report cache dir %q, got %q", want, got)
	}
}

func TestSecurityMetadataCacheFileUsesVersionedCacheSubdir(t *testing.T) {
	cacheHome := filepath.Join(t.TempDir(), "cache")
	t.Setenv("XDG_CACHE_HOME", cacheHome)

	want := filepath.Join(cacheHome, "updev", "security-metadata-v1", "github-repo", "abc.json")
	if got := SecurityMetadataCacheFile("github-repo", "abc"); got != want {
		t.Fatalf("expected security metadata cache path %q, got %q", want, got)
	}
	if got := SecurityMetadataCacheFile("", "abc"); got != "" {
		t.Fatalf("expected empty kind to return empty path, got %q", got)
	}
	if got := SecurityMetadataCacheFile("github-repo", ""); got != "" {
		t.Fatalf("expected empty key to return empty path, got %q", got)
	}
}

func TestDataHomeUsesXDGDataHome(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "data")
	t.Setenv("XDG_DATA_HOME", dataHome)

	if got := DataHome(); got != dataHome {
		t.Fatalf("expected XDG data home %q, got %q", dataHome, got)
	}
}

func TestDataHomeFallsBackToHomeLocalShare(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", "")
	home := HomeDir()
	if home == "" {
		t.Skip("home directory unavailable")
	}

	want := filepath.Join(home, ".local", "share")
	if got := DataHome(); got != want {
		t.Fatalf("expected home data dir %q, got %q", want, got)
	}
}

func TestInventoryCacheFileUsesConfiguredStateDir(t *testing.T) {
	stateDir := "state/updev"

	want := filepath.Join("/root", stateDir, "inventory-v1.json")
	if got := InventoryCacheFile("/root", &stateDir); got != want {
		t.Fatalf("expected configured inventory cache file %q, got %q", want, got)
	}
}

func TestHomeOrRootBrewfilePrefersExistingHomeBrewfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "Brewfile"), []byte("brew \"git\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(home, "Brewfile")
	if got := HomeOrRootBrewfile("/root"); got != want {
		t.Fatalf("expected home Brewfile %q, got %q", want, got)
	}
}

func TestHomeOrRootBrewfileFallsBackToRootTemplate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := RootBrewfileTemplate("/root")
	if got := HomeOrRootBrewfile("/root"); got != want {
		t.Fatalf("expected root Brewfile template %q, got %q", want, got)
	}
}

func TestRootBrewfileTemplate(t *testing.T) {
	want := filepath.Join("/root", "Brewfile.tmpl")
	if got := RootBrewfileTemplate("/root"); got != want {
		t.Fatalf("expected root Brewfile template %q, got %q", want, got)
	}
}

func TestDefaultChezmoiSourceRootUsesHome(t *testing.T) {
	home := HomeDir()
	if home == "" {
		t.Skip("home directory unavailable")
	}

	want := filepath.Join(home, ".local", "share", "chezmoi")
	if got := DefaultChezmoiSourceRoot(); got != want {
		t.Fatalf("expected default source root %q, got %q", want, got)
	}
}

func TestDefaultRootPrecedence(t *testing.T) {
	t.Setenv("UPDEV_ROOT", "/env-root")
	t.Setenv("CHEZMOI_SOURCE_DIR", "/chezmoi-root")

	if got := DefaultRoot("/config-root"); got != "/env-root" {
		t.Fatalf("expected UPDEV_ROOT to win, got %q", got)
	}

	t.Setenv("UPDEV_ROOT", "")
	if got := DefaultRoot("/config-root"); got != "/config-root" {
		t.Fatalf("expected configured root to win, got %q", got)
	}

	if got := DefaultRoot(""); got != "/chezmoi-root" {
		t.Fatalf("expected CHEZMOI_SOURCE_DIR fallback, got %q", got)
	}
}

func TestDefaultRootFallsBackToCWD(t *testing.T) {
	t.Setenv("UPDEV_ROOT", "")
	t.Setenv("CHEZMOI_SOURCE_DIR", "")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	if got := DefaultRoot(""); got != cwd {
		t.Fatalf("expected cwd fallback %q, got %q", cwd, got)
	}
}

func TestResolveHandlesTildeAbsoluteAndRelative(t *testing.T) {
	home := HomeDir()
	if home == "" {
		t.Skip("home directory unavailable")
	}

	if got := Resolve("/root", "~/file"); got != filepath.Join(home, "file") {
		t.Fatalf("expected tilde path under home, got %q", got)
	}
	if got := Resolve("/root", "/abs/file"); got != "/abs/file" {
		t.Fatalf("expected absolute path, got %q", got)
	}
	if got := Resolve("/root", "relative/file"); got != filepath.Join("/root", "relative/file") {
		t.Fatalf("expected root-relative path, got %q", got)
	}
}

func TestResolveConfigRelative(t *testing.T) {
	configFile := filepath.Join("/configs", "updev", "config.toml")

	if got := ResolveConfigRelative("roots/main", configFile); got != filepath.Join("/configs", "updev", "roots/main") {
		t.Fatalf("expected config-relative path, got %q", got)
	}
	if got := ResolveConfigRelative("roots/main", ""); got != filepath.Clean("roots/main") {
		t.Fatalf("expected clean relative path without config, got %q", got)
	}
	if got := ResolveConfigRelative("/root", configFile); got != "/root" {
		t.Fatalf("expected absolute path, got %q", got)
	}
}
