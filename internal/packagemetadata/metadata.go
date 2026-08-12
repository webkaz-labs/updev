package packagemetadata

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/BurntSushi/toml"
)

const (
	SchemaVersion                  = 1
	DiagnosticStalePackageMetadata = "stale-package-metadata"
)

var identitySegmentPattern = regexp.MustCompile(`^[a-z0-9._-]+$`)

type Identity struct {
	Provider string
	Kind     string
	Name     string
}

func (identity Identity) String() string {
	return identity.Provider + "/" + identity.Kind + "/" + identity.Name
}

type Package struct {
	Identity             string           `json:"identity"`
	Reason               string           `json:"reason,omitempty"`
	Lifecycle            string           `json:"lifecycle,omitempty"`
	Executor             string           `json:"executor,omitempty"`
	IntentionalDuplicate *bool            `json:"intentional_duplicate,omitempty"`
	Homebrew             *HomebrewOptions `json:"homebrew,omitempty"`
}

type HomebrewOptions struct {
	Link *bool `toml:"link" json:"link,omitempty"`
}

type Diagnostic struct {
	Code     string `json:"code"`
	Identity string `json:"identity"`
	Detail   string `json:"detail"`
}

type Set struct {
	Path        string       `json:"path"`
	Packages    []Package    `json:"packages"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type document struct {
	SchemaVersion int                   `toml:"schema_version"`
	Packages      map[string]rawPackage `toml:"packages"`
}

type rawPackage struct {
	Reason               string           `toml:"reason"`
	Lifecycle            string           `toml:"lifecycle"`
	Executor             string           `toml:"executor"`
	IntentionalDuplicate *bool            `toml:"intentional_duplicate"`
	Homebrew             *HomebrewOptions `toml:"homebrew"`
}

func ParseIdentity(value string) (Identity, error) {
	if value == "" || value != strings.TrimSpace(value) {
		return Identity{}, fmt.Errorf("canonical package identity must not be empty or padded")
	}
	provider, rest, ok := strings.Cut(value, "/")
	if !ok {
		return Identity{}, fmt.Errorf("canonical package identity %q must use provider/kind/name", value)
	}
	kind, name, ok := strings.Cut(rest, "/")
	if !ok || name == "" {
		return Identity{}, fmt.Errorf("canonical package identity %q must use provider/kind/name", value)
	}
	if !identitySegmentPattern.MatchString(provider) {
		return Identity{}, fmt.Errorf("canonical package identity %q has invalid provider %q", value, provider)
	}
	if !identitySegmentPattern.MatchString(kind) {
		return Identity{}, fmt.Errorf("canonical package identity %q has invalid kind %q", value, kind)
	}
	if strings.IndexFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return Identity{}, fmt.Errorf("canonical package identity %q has whitespace or control characters in name", value)
	}
	return Identity{Provider: provider, Kind: kind, Name: name}, nil
}

func Load(path string, desiredIDs []string) (Set, error) {
	desired, err := canonicalSet(desiredIDs)
	if err != nil {
		return Set{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Set{Path: path, Packages: []Package{}, Diagnostics: []Diagnostic{}}, nil
		}
		return Set{}, fmt.Errorf("read package metadata %s: %w", path, err)
	}
	return parse(path, data, desired)
}

func Parse(path string, data []byte, desiredIDs []string) (Set, error) {
	desired, err := canonicalSet(desiredIDs)
	if err != nil {
		return Set{}, err
	}
	return parse(path, data, desired)
}

func parse(path string, data []byte, desired map[string]struct{}) (Set, error) {
	var decoded document
	metadata, err := toml.Decode(string(data), &decoded)
	if err != nil {
		return Set{}, fmt.Errorf("parse package metadata %s: %w", path, err)
	}
	if undecoded := metadata.Undecoded(); len(undecoded) > 0 {
		keys := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			keys = append(keys, key.String())
		}
		sort.Strings(keys)
		return Set{}, fmt.Errorf("package metadata %s has unknown keys: %s", path, strings.Join(keys, ", "))
	}
	if decoded.SchemaVersion != SchemaVersion {
		return Set{}, fmt.Errorf("package metadata %s requires schema_version = %d", path, SchemaVersion)
	}
	if decoded.Packages == nil {
		decoded.Packages = map[string]rawPackage{}
	}

	identities := make([]string, 0, len(decoded.Packages))
	for value := range decoded.Packages {
		identity, identityErr := ParseIdentity(value)
		if identityErr != nil {
			return Set{}, fmt.Errorf("package metadata %s: %w", path, identityErr)
		}
		identities = append(identities, identity.String())
	}
	sort.Strings(identities)

	result := Set{Path: path, Packages: make([]Package, 0, len(identities)), Diagnostics: []Diagnostic{}}
	for _, identity := range identities {
		raw := decoded.Packages[identity]
		pkg, packageErr := normalizePackage(identity, raw)
		if packageErr != nil {
			return Set{}, fmt.Errorf("package metadata %s: %w", path, packageErr)
		}
		result.Packages = append(result.Packages, pkg)
		if _, ok := desired[identity]; !ok {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{
				Code:     DiagnosticStalePackageMetadata,
				Identity: identity,
				Detail:   "package metadata has no matching active desired package",
			})
		}
	}
	return result, nil
}

func normalizePackage(identity string, raw rawPackage) (Package, error) {
	reason, err := optionalText(identity, "reason", raw.Reason)
	if err != nil {
		return Package{}, err
	}
	lifecycle, err := optionalText(identity, "lifecycle", raw.Lifecycle)
	if err != nil {
		return Package{}, err
	}
	executor := strings.TrimSpace(raw.Executor)
	if raw.Executor != executor {
		return Package{}, fmt.Errorf("package %s executor must not be padded", identity)
	}
	switch executor {
	case "", "auto", "mise", "native":
	default:
		return Package{}, fmt.Errorf("package %s has invalid executor %q", identity, executor)
	}
	if raw.Homebrew != nil && raw.Homebrew.Link == nil {
		return Package{}, fmt.Errorf("package %s has an empty homebrew option table", identity)
	}
	if reason == "" && lifecycle == "" && executor == "" && raw.IntentionalDuplicate == nil && raw.Homebrew == nil {
		return Package{}, fmt.Errorf("package %s has no metadata", identity)
	}
	return Package{
		Identity:             identity,
		Reason:               reason,
		Lifecycle:            lifecycle,
		Executor:             executor,
		IntentionalDuplicate: raw.IntentionalDuplicate,
		Homebrew:             raw.Homebrew,
	}, nil
}

func optionalText(identity string, field string, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("package %s %s must be non-empty, unpadded text", identity, field)
	}
	return value, nil
}

func canonicalSet(values []string) (map[string]struct{}, error) {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		identity, err := ParseIdentity(value)
		if err != nil {
			return nil, fmt.Errorf("desired package identity: %w", err)
		}
		canonical := identity.String()
		if _, exists := result[canonical]; exists {
			return nil, fmt.Errorf("duplicate desired package identity: %s", canonical)
		}
		result[canonical] = struct{}{}
	}
	return result, nil
}
