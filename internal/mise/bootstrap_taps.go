package mise

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

type BootstrapTapDesired struct {
	Identity string   `json:"identity"`
	Name     string   `json:"name"`
	URL      string   `json:"url,omitempty"`
	Sources  []string `json:"sources"`
}

type bootstrapTapDocument struct {
	Bootstrap struct {
		Brew struct {
			Taps map[string]string `toml:"taps"`
		} `toml:"brew"`
	} `toml:"bootstrap"`
}

// BootstrapTapsFromSources reads only the tap declarations from config files
// already selected by mise config resolution. Mise v2026.8.2 does not expose
// these declarations in bootstrap status JSON.
func BootstrapTapsFromSources(sources []ConfigSource) ([]BootstrapTapDesired, error) {
	byName := map[string]*BootstrapTapDesired{}
	for _, source := range sources {
		data, err := os.ReadFile(source.Path)
		if err != nil {
			return nil, fmt.Errorf("read mise bootstrap tap source %s: %w", source.Path, err)
		}
		var document bootstrapTapDocument
		if _, err := toml.Decode(string(data), &document); err != nil {
			return nil, fmt.Errorf("parse mise bootstrap tap source %s: %w", source.Path, err)
		}
		names := make([]string, 0, len(document.Bootstrap.Brew.Taps))
		for rawName := range document.Bootstrap.Brew.Taps {
			names = append(names, rawName)
		}
		sort.Strings(names)
		for _, rawName := range names {
			name := strings.TrimSpace(rawName)
			if name == "" || name != rawName || strings.IndexFunc(name, func(r rune) bool {
				return unicode.IsSpace(r) || unicode.IsControl(r)
			}) >= 0 {
				return nil, fmt.Errorf("mise bootstrap tap name %q in %s is invalid", rawName, source.Path)
			}
			url := strings.TrimSpace(document.Bootstrap.Brew.Taps[rawName])
			if existing, ok := byName[name]; ok {
				existing.Sources = append(existing.Sources, source.Path)
				continue
			}
			byName[name] = &BootstrapTapDesired{
				Identity: "brew-tap:" + name,
				Name:     name,
				URL:      url,
				Sources:  []string{source.Path},
			}
		}
	}

	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]BootstrapTapDesired, 0, len(names))
	for _, name := range names {
		result = append(result, *byName[name])
	}
	return result, nil
}
