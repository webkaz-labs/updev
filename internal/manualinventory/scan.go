package manualinventory

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/webkaz-labs/updev/internal/manualinventory/platform"
)

type App struct {
	Name          string
	Source        string
	Path          string
	BundleID      string
	IdentifierKey string
	Identifier    string
	Version       string
}

type Scanner func(root string, defaultRoot string) []App

func ScanApplications(root string, defaultRoot string) []App {
	apps := []App{}
	seen := map[string]bool{}
	for _, scanner := range ApplicationScanners() {
		for _, app := range scanner(root, defaultRoot) {
			key := AppKey(app)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			apps = append(apps, app)
		}
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Name != apps[j].Name {
			return apps[i].Name < apps[j].Name
		}
		return apps[i].Path < apps[j].Path
	})
	return apps
}

func ApplicationScanners() []Scanner {
	return []Scanner{
		func(root string, defaultRoot string) []App {
			return appsFromPlatform(platform.ScanMacApplications(root, defaultRoot))
		},
		func(root string, defaultRoot string) []App {
			return appsFromPlatform(platform.ScanLinuxApplications(root, defaultRoot))
		},
		func(root string, defaultRoot string) []App {
			return appsFromPlatform(platform.ScanWindowsApplications(root, defaultRoot))
		},
	}
}

func appsFromPlatform(apps []platform.App) []App {
	out := make([]App, 0, len(apps))
	for _, app := range apps {
		out = append(out, App{
			Name:          app.Name,
			Source:        app.Source,
			Path:          app.Path,
			BundleID:      app.BundleID,
			IdentifierKey: app.IdentifierKey,
			Identifier:    app.Identifier,
			Version:       app.Version,
		})
	}
	return out
}

func AppKey(app App) string {
	if app.IdentifierKey != "" && app.Identifier != "" {
		if app.IdentifierKey == "bundle_id" {
			return "bundle:" + strings.ToLower(app.Identifier)
		}
		if app.IdentifierKey == "path" {
			return "path:" + filepath.Clean(app.Identifier)
		}
		return strings.ToLower(app.IdentifierKey) + ":" + strings.ToLower(app.Identifier)
	}
	if app.BundleID != "" {
		return "bundle:" + strings.ToLower(app.BundleID)
	}
	if app.Path != "" {
		return "path:" + filepath.Clean(app.Path)
	}
	return "name:" + normalizedAppKey(app.Name)
}

func normalizedAppKey(name string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
