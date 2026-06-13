package securityscanner

import (
	"net/url"
	"strings"
)

type PackageInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
}

func PackageInfoFromPURL(rawPURL string, fallbackName string, fallbackVersion string, fallbackType string) PackageInfo {
	pkg := ParsePURL(rawPURL)
	if pkg.Name == "" {
		pkg.Name = strings.TrimSpace(fallbackName)
	}
	if pkg.Version == "" {
		pkg.Version = strings.TrimSpace(fallbackVersion)
	}
	if pkg.Ecosystem == "" {
		pkg.Ecosystem = EcosystemFromType(fallbackType)
	}
	return pkg
}

func ParsePURL(rawPURL string) PackageInfo {
	value := strings.TrimSpace(rawPURL)
	if value == "" || !strings.HasPrefix(strings.ToLower(value), "pkg:") {
		return PackageInfo{}
	}
	value = value[len("pkg:"):]
	if index := strings.IndexAny(value, "?#"); index >= 0 {
		value = value[:index]
	}
	purlType, path, ok := strings.Cut(value, "/")
	if !ok {
		return PackageInfo{Ecosystem: EcosystemFromType(purlType)}
	}
	version := ""
	if index := strings.LastIndex(path, "@"); index > 0 {
		version = URLUnescape(path[index+1:])
		path = path[:index]
	}
	return PackageInfo{
		Name:      PackageNameFromPURLPath(purlType, path),
		Version:   version,
		Ecosystem: EcosystemFromType(purlType),
	}
}

func PackageNameFromPURLPath(purlType string, path string) string {
	parts := []string{}
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		if part == "" {
			continue
		}
		parts = append(parts, URLUnescape(part))
	}
	if len(parts) == 0 {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(purlType)) {
	case "maven":
		if len(parts) >= 2 {
			return parts[len(parts)-2] + ":" + parts[len(parts)-1]
		}
	case "npm":
		if len(parts) >= 2 && strings.HasPrefix(parts[len(parts)-2], "@") {
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
	case "composer":
		if len(parts) >= 2 {
			return parts[len(parts)-2] + "/" + parts[len(parts)-1]
		}
	}
	return parts[len(parts)-1]
}

func URLUnescape(value string) string {
	decoded, err := url.PathUnescape(value)
	if err != nil {
		return value
	}
	return decoded
}

func EcosystemFromType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "npm", "node-pkg":
		return "npm"
	case "cargo", "rust":
		return "crates.io"
	case "pypi", "python":
		return "PyPI"
	case "golang", "go":
		return "Go"
	case "maven", "java":
		return "Maven"
	case "gem", "ruby", "rubygems":
		return "RubyGems"
	case "nuget", "dotnet":
		return "NuGet"
	case "composer", "php":
		return "Packagist"
	default:
		return ""
	}
}
