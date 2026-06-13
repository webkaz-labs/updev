package manualinventory

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/webkaz-labs/updev/internal/updevpath"
)

type App struct {
	Name     string
	Source   string
	Path     string
	BundleID string
	Version  string
}

func ScanMacApplications(root string, defaultRoot string) []App {
	return ScanMacApplicationRoots(MacApplicationRoots(root, defaultRoot))
}

func MacApplicationRoots(root string, defaultRoot string) []string {
	if filepath.Clean(root) != filepath.Clean(defaultRoot) {
		return []string{
			filepath.Join(root, "Applications"),
			filepath.Join(root, "home", "Applications"),
		}
	}
	if runtime.GOOS != "darwin" {
		return nil
	}
	roots := []string{"/Applications"}
	if home := updevpath.HomeDir(); home != "" {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	return roots
}

func ScanMacApplicationRoots(roots []string) []App {
	apps := []App{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".app") {
				continue
			}
			apps = append(apps, readMacAppBundle(filepath.Join(root, entry.Name())))
		}
	}
	return apps
}

func readMacAppBundle(path string) App {
	fallbackName := strings.TrimSuffix(filepath.Base(path), ".app")
	values := readMacInfoPlistStrings(filepath.Join(path, "Contents", "Info.plist"))
	name := firstNonEmpty(values["CFBundleDisplayName"], values["CFBundleName"], fallbackName)
	source := "app bundle"
	if macAppHasMASReceipt(path) {
		source = "mac app store receipt"
	}
	return App{
		Name:     name,
		Source:   source,
		Path:     filepath.Clean(path),
		BundleID: values["CFBundleIdentifier"],
		Version:  firstNonEmpty(values["CFBundleShortVersionString"], values["CFBundleVersion"]),
	}
}

func macAppHasMASReceipt(path string) bool {
	info, err := os.Stat(filepath.Join(path, "Contents", "_MASReceipt", "receipt"))
	return err == nil && !info.IsDir()
}

func readMacInfoPlistStrings(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	values := map[string]string{}
	currentKey := ""
	for {
		token, err := decoder.Token()
		if err != nil {
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "key":
			if text, ok := readMacPlistElementText(decoder, "key"); ok {
				currentKey = text
			}
		case "string":
			if currentKey == "" {
				continue
			}
			if text, ok := readMacPlistElementText(decoder, "string"); ok {
				values[currentKey] = text
				currentKey = ""
			}
		default:
			currentKey = ""
		}
	}
	return values
}

func readMacPlistElementText(decoder *xml.Decoder, element string) (string, bool) {
	var builder strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", false
		}
		switch typed := token.(type) {
		case xml.CharData:
			builder.Write([]byte(typed))
		case xml.EndElement:
			return strings.TrimSpace(builder.String()), typed.Name.Local == element
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
