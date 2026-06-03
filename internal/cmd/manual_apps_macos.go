package cmd

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func scanManualMacApplications(root string) []manualScannedApp {
	return scanManualMacApplicationRoots(manualMacApplicationRoots(root))
}

func manualMacApplicationRoots(root string) []string {
	if filepath.Clean(root) != filepath.Clean(defaultRoot()) {
		return []string{
			filepath.Join(root, "Applications"),
			filepath.Join(root, "home", "Applications"),
		}
	}
	if runtime.GOOS != "darwin" {
		return nil
	}
	roots := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	return roots
}

func scanManualMacApplicationRoots(roots []string) []manualScannedApp {
	apps := []manualScannedApp{}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasSuffix(entry.Name(), ".app") {
				continue
			}
			apps = append(apps, readManualMacAppBundle(filepath.Join(root, entry.Name())))
		}
	}
	return apps
}

func readManualMacAppBundle(path string) manualScannedApp {
	fallbackName := strings.TrimSuffix(filepath.Base(path), ".app")
	values := readManualMacInfoPlistStrings(filepath.Join(path, "Contents", "Info.plist"))
	name := firstNonEmptyManualValue(values["CFBundleDisplayName"], values["CFBundleName"], fallbackName)
	source := "app bundle"
	if manualMacAppHasMASReceipt(path) {
		source = "mac app store receipt"
	}
	return manualScannedApp{
		Name:     name,
		Source:   source,
		Path:     filepath.Clean(path),
		BundleID: values["CFBundleIdentifier"],
		Version:  firstNonEmptyManualValue(values["CFBundleShortVersionString"], values["CFBundleVersion"]),
	}
}

func manualMacAppHasMASReceipt(path string) bool {
	info, err := os.Stat(filepath.Join(path, "Contents", "_MASReceipt", "receipt"))
	return err == nil && !info.IsDir()
}

func readManualMacInfoPlistStrings(path string) map[string]string {
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
			if text, ok := readManualMacPlistElementText(decoder, "key"); ok {
				currentKey = text
			}
		case "string":
			if currentKey == "" {
				continue
			}
			if text, ok := readManualMacPlistElementText(decoder, "string"); ok {
				values[currentKey] = text
				currentKey = ""
			}
		default:
			currentKey = ""
		}
	}
	return values
}

func readManualMacPlistElementText(decoder *xml.Decoder, element string) (string, bool) {
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
