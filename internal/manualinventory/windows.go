package manualinventory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func ScanWindowsApplications(root string, defaultRoot string) []App {
	paths := WindowsWingetExportPaths(root, defaultRoot)
	apps := []App{}
	for _, path := range paths {
		apps = append(apps, scanWindowsWingetExport(path)...)
	}
	return apps
}

func WindowsWingetExportPaths(root string, defaultRoot string) []string {
	if filepath.Clean(root) != filepath.Clean(defaultRoot) {
		return []string{
			filepath.Join(root, "winget-export.json"),
			filepath.Join(root, "ProgramData", "updev", "winget-export.json"),
		}
	}
	if runtime.GOOS != "windows" {
		return nil
	}
	return []string{filepath.Join(root, "winget-export.json")}
}

type wingetExport struct {
	Sources []struct {
		Packages []struct {
			PackageIdentifier string `json:"PackageIdentifier"`
			Version           string `json:"Version"`
		} `json:"Packages"`
	} `json:"Sources"`
}

func scanWindowsWingetExport(path string) []App {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var payload wingetExport
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	apps := []App{}
	for _, source := range payload.Sources {
		for _, pkg := range source.Packages {
			id := strings.TrimSpace(pkg.PackageIdentifier)
			if id == "" {
				continue
			}
			apps = append(apps, App{
				Name:          wingetDisplayName(id),
				Source:        "winget export",
				Path:          filepath.Clean(path),
				IdentifierKey: "package_id",
				Identifier:    id,
				Version:       strings.TrimSpace(pkg.Version),
			})
		}
	}
	return apps
}

func wingetDisplayName(identifier string) string {
	parts := strings.Split(identifier, ".")
	if len(parts) == 0 {
		return identifier
	}
	return parts[len(parts)-1]
}
