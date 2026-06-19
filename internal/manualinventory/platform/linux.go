package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/webkaz-labs/updev/internal/updevpath"
)

func ScanLinuxApplications(root string, defaultRoot string) []App {
	roots := LinuxApplicationRoots(root, defaultRoot)
	apps := []App{}
	apps = append(apps, scanLinuxDesktopEntries(roots.DesktopEntries)...)
	apps = append(apps, scanLinuxFlatpakApps(roots.FlatpakApps)...)
	apps = append(apps, scanLinuxSnapApps(roots.SnapApps)...)
	apps = append(apps, scanLinuxAppImages(roots.AppImages)...)
	return apps
}

type LinuxRoots struct {
	DesktopEntries []string
	FlatpakApps    []string
	SnapApps       []string
	AppImages      []string
}

func LinuxApplicationRoots(root string, defaultRoot string) LinuxRoots {
	if filepath.Clean(root) != filepath.Clean(defaultRoot) {
		return LinuxRoots{
			DesktopEntries: []string{
				filepath.Join(root, "usr", "share", "applications"),
				filepath.Join(root, "home", ".local", "share", "applications"),
			},
			FlatpakApps: []string{
				filepath.Join(root, "var", "lib", "flatpak", "app"),
				filepath.Join(root, "home", ".local", "share", "flatpak", "app"),
			},
			SnapApps: []string{
				filepath.Join(root, "snap"),
			},
			AppImages: []string{
				filepath.Join(root, "home", "Applications"),
				filepath.Join(root, "home", ".local", "bin"),
			},
		}
	}
	if runtime.GOOS != "linux" {
		return LinuxRoots{}
	}
	home := updevpath.HomeDir()
	roots := LinuxRoots{
		DesktopEntries: []string{"/usr/share/applications"},
		FlatpakApps:    []string{"/var/lib/flatpak/app"},
		SnapApps:       []string{"/snap"},
	}
	if home != "" {
		roots.DesktopEntries = append(roots.DesktopEntries, filepath.Join(home, ".local", "share", "applications"))
		roots.FlatpakApps = append(roots.FlatpakApps, filepath.Join(home, ".local", "share", "flatpak", "app"))
		roots.AppImages = append(roots.AppImages, filepath.Join(home, "Applications"), filepath.Join(home, ".local", "bin"))
	}
	return roots
}

func scanLinuxDesktopEntries(roots []string) []App {
	apps := []App{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".desktop") {
				continue
			}
			if app, ok := readLinuxDesktopEntry(filepath.Join(root, entry.Name())); ok {
				apps = append(apps, app)
			}
		}
	}
	return apps
}

func readLinuxDesktopEntry(path string) (App, bool) {
	values := readDesktopEntryValues(path)
	name := firstNonEmpty(values["Name"], strings.TrimSuffix(filepath.Base(path), ".desktop"))
	if name == "" {
		return App{}, false
	}
	desktopID := strings.TrimSuffix(filepath.Base(path), ".desktop")
	source := "desktop entry"
	identifierKey := "desktop_id"
	identifier := desktopID
	if flatpakID := firstNonEmpty(values["X-Flatpak"], values["X-Flatpak-RenamedFrom"]); flatpakID != "" {
		source = "flatpak desktop entry"
		identifierKey = "package_id"
		identifier = flatpakID
	}
	if snapID := firstNonEmpty(values["X-SnapInstanceName"], values["X-SnapAppName"]); snapID != "" {
		source = "snap desktop entry"
		identifierKey = "package_id"
		identifier = snapID
	}
	return App{
		Name:          name,
		Source:        source,
		Path:          filepath.Clean(path),
		IdentifierKey: identifierKey,
		Identifier:    identifier,
		Version:       strings.TrimSpace(values["X-Version"]),
	}, true
}

func readDesktopEntryValues(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	values := map[string]string{}
	inDesktopEntry := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inDesktopEntry = strings.EqualFold(strings.Trim(line, "[]"), "Desktop Entry")
			continue
		}
		if !inDesktopEntry {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return values
}

func scanLinuxFlatpakApps(roots []string) []App {
	apps := []App{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			id := entry.Name()
			apps = append(apps, App{
				Name:          id,
				Source:        "flatpak metadata",
				Path:          filepath.Join(root, id),
				IdentifierKey: "package_id",
				Identifier:    id,
			})
		}
	}
	return apps
}

func scanLinuxSnapApps(roots []string) []App {
	apps := []App{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			id := entry.Name()
			apps = append(apps, App{
				Name:          id,
				Source:        "snap package",
				Path:          filepath.Join(root, id),
				IdentifierKey: "package_id",
				Identifier:    id,
			})
		}
	}
	return apps
}

func scanLinuxAppImages(roots []string) []App {
	apps := []App{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".appimage") {
				continue
			}
			name := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
			path := filepath.Clean(filepath.Join(root, entry.Name()))
			apps = append(apps, App{
				Name:          name,
				Source:        "appimage file",
				Path:          path,
				IdentifierKey: "path",
				Identifier:    path,
			})
		}
	}
	return apps
}
