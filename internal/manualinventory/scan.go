package manualinventory

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

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
			return ScanMacApplications(root, defaultRoot)
		},
	}
}

func AppKey(app App) string {
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
