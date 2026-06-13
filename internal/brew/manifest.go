package brew

import (
	"bufio"
	"io"
	"regexp"
	"sort"
	"strings"
)

type Manifest struct {
	entries map[string]ManifestEntry
}

type ManifestEntry struct {
	Kind     string
	Name     string
	RawName  string
	Source   string
	Tap      string
	URLBased bool
}

func ParseManifest(reader io.Reader, source string) (Manifest, error) {
	manifest := Manifest{entries: map[string]ManifestEntry{}}
	lineRe := regexp.MustCompile(`^\s*(brew|cask|tap|vscode)\s+"([^"]+)"`)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		match := lineRe.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		kind := match[1]
		if kind != "brew" && kind != "cask" {
			continue
		}
		rawName := strings.TrimSpace(match[2])
		entry := ManifestEntry{
			Kind:     kind,
			Name:     NormalizePackageName(kind, rawName),
			RawName:  rawName,
			Source:   source,
			Tap:      TapName(kind, rawName),
			URLBased: IsURLName(rawName),
		}
		manifest.entries[manifestKey(kind, entry.Name)] = entry
	}
	return manifest, scanner.Err()
}

func (manifest Manifest) Entry(kind string, name string) ManifestEntry {
	if manifest.entries == nil {
		return ManifestEntry{}
	}
	return manifest.entries[manifestKey(kind, NormalizePackageName(kind, name))]
}

func (manifest Manifest) Entries() []ManifestEntry {
	if manifest.entries == nil {
		return nil
	}
	entries := make([]ManifestEntry, 0, len(manifest.entries))
	for _, entry := range manifest.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Name != entries[j].Name {
			return entries[i].Name < entries[j].Name
		}
		return entries[i].Kind < entries[j].Kind
	})
	return entries
}

func NormalizePackageName(kind string, name string) string {
	name = strings.TrimSpace(name)
	if (kind == "brew" || kind == "cask") && strings.Contains(name, "/") && !IsURLName(name) {
		parts := strings.Split(name, "/")
		return parts[len(parts)-1]
	}
	return name
}

func TapName(kind string, name string) string {
	if kind != "brew" && kind != "cask" {
		return ""
	}
	if !strings.Contains(name, "/") || IsURLName(name) {
		return ""
	}
	parts := strings.Split(name, "/")
	if len(parts) < 3 {
		return ""
	}
	return strings.Join(parts[:len(parts)-1], "/")
}

func IsOfficialTap(tap string) bool {
	return tap == "" || strings.HasPrefix(tap, "homebrew/")
}

func IsURLName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "file://")
}

func manifestKey(kind string, name string) string {
	return kind + ":" + name
}
