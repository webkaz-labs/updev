package updevpath

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	ToolName = "updev"
)

func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func ConfigHome() string {
	if value := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); value != "" {
		return value
	}
	if home := HomeDir(); home != "" {
		return filepath.Join(home, ".config")
	}
	return ""
}

func CacheHome() string {
	if value := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); value != "" {
		return value
	}
	if home := HomeDir(); home != "" {
		return filepath.Join(home, ".cache")
	}
	return ""
}

func DataHome() string {
	if value := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); value != "" {
		return value
	}
	if home := HomeDir(); home != "" {
		return filepath.Join(home, ".local", "share")
	}
	return ""
}

func ConfigFile() string {
	if value := strings.TrimSpace(os.Getenv("UPDEV_CONFIG")); value != "" {
		return value
	}
	if base := ConfigHome(); base != "" {
		return filepath.Join(base, ToolName, "config.toml")
	}
	return ""
}

func SecurityPolicyFile() string {
	if value := strings.TrimSpace(os.Getenv("UPDEV_SECURITY_POLICY")); value != "" {
		return value
	}
	if base := ConfigHome(); base != "" {
		return filepath.Join(base, ToolName, "security-policy.json")
	}
	return ""
}

func InventoryOverridesFile() string {
	if base := ConfigHome(); base != "" {
		return filepath.Join(base, ToolName, "inventory-overrides.toml")
	}
	return ""
}

func CacheDir() string {
	if base := CacheHome(); base != "" {
		return filepath.Join(base, ToolName)
	}
	return ""
}

func ReportCacheDir() string {
	return CacheSubdir("reports")
}

func SecurityMetadataCacheFile(kind string, key string) string {
	if strings.TrimSpace(kind) == "" || strings.TrimSpace(key) == "" {
		return ""
	}
	return CacheFile("security-metadata-v1", kind, key+".json")
}

func CacheSubdir(parts ...string) string {
	dir := CacheDir()
	if dir == "" {
		return ""
	}
	cleaned := cleanPathParts(parts...)
	if len(cleaned) == 0 {
		return dir
	}
	return filepath.Join(append([]string{dir}, cleaned...)...)
}

func CacheFile(parts ...string) string {
	cleaned := cleanPathParts(parts...)
	if len(cleaned) == 0 {
		return ""
	}
	return CacheSubdir(cleaned...)
}

func InventoryCacheFile(root string, configuredStateDir *string) string {
	if configuredStateDir != nil {
		if path := Resolve(root, *configuredStateDir); path != "" {
			return filepath.Join(path, "inventory-v1.json")
		}
	}
	if dir := CacheDir(); dir != "" {
		return filepath.Join(dir, "inventory-v1.json")
	}
	return ""
}

func HomeOrRootBrewfile(root string) string {
	if home := HomeDir(); home != "" {
		path := filepath.Join(home, "Brewfile")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return RootBrewfileTemplate(root)
}

func RootBrewfileTemplate(root string) string {
	return filepath.Join(root, "Brewfile.tmpl")
}

func DefaultRoot(configured string) string {
	if root := strings.TrimSpace(os.Getenv("UPDEV_ROOT")); root != "" {
		return root
	}
	if root := strings.TrimSpace(configured); root != "" {
		return root
	}
	if root := strings.TrimSpace(os.Getenv("CHEZMOI_SOURCE_DIR")); root != "" {
		return root
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}

func DefaultChezmoiSourceRoot() string {
	if home := HomeDir(); home != "" {
		return filepath.Join(home, ".local", "share", "chezmoi")
	}
	return filepath.Join(".local", "share", "chezmoi")
}

func Resolve(root string, path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") {
		if home := HomeDir(); home != "" {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func ResolveConfigRelative(path string, configFile string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(filepath.Clean(path), "~/") || filepath.IsAbs(path) {
		return Resolve(".", path)
	}
	if configFile == "" {
		return filepath.Clean(path)
	}
	return filepath.Join(filepath.Dir(configFile), path)
}

func cleanPathParts(parts ...string) []string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil
		}
		cleaned = append(cleaned, part)
	}
	return cleaned
}
