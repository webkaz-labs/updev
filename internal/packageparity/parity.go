package packageparity

import (
	"fmt"
	"sort"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
)

const SchemaVersion = 1

const (
	ParityMatch        = "match"
	ParityBrewfileOnly = "brewfile-only"
	ParityMiseOnly     = "mise-only"
)

type Report struct {
	SchemaVersion       int                 `json:"schema_version"`
	Status              plan.Status         `json:"status"`
	Root                string              `json:"root"`
	BrewfilePath        string              `json:"brewfile_path"`
	MiseSources         []mise.ConfigSource `json:"mise_sources"`
	Summary             Summary             `json:"summary"`
	Items               []Item              `json:"items"`
	IgnoredMiseManagers map[string]int      `json:"ignored_mise_managers,omitempty"`
}

type Summary struct {
	Matched      int `json:"matched"`
	BrewfileOnly int `json:"brewfile_only"`
	MiseOnly     int `json:"mise_only"`
}

type Item struct {
	Identity          string `json:"identity"`
	Kind              string `json:"kind"`
	Name              string `json:"name"`
	Parity            string `json:"parity"`
	BrewfileDesired   bool   `json:"brewfile_desired"`
	MiseDesired       bool   `json:"mise_desired"`
	RequestedVersion  string `json:"requested_version,omitempty"`
	ManagerAvailable  *bool  `json:"manager_available,omitempty"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

func Build(root string, brewfilePath string, brewfileItems []plan.Item, packageSet mise.BootstrapPackageSet, taps []mise.BootstrapTapDesired) (Report, error) {
	entries := map[string]*Item{}
	for _, item := range brewfileItems {
		identity, kind, _, ok := CanonicalBrewfileItem(item)
		if !ok {
			continue
		}
		entry := ensureEntry(entries, identity, kind, item.Name)
		entry.BrewfileDesired = true
	}

	ignored := map[string]int{}
	for _, pkg := range packageSet.Packages {
		identity, kind, name, ok := CanonicalHomebrewPackage(pkg.Manager, pkg.Name)
		if !ok {
			ignored[pkg.Manager]++
			continue
		}
		entry := ensureEntry(entries, identity, kind, name)
		if entry.MiseDesired {
			return Report{}, fmt.Errorf("duplicate normalized mise package identity: %s", identity)
		}
		available := pkg.ManagerAvailable
		entry.MiseDesired = true
		entry.RequestedVersion = pkg.RequestedVersion
		entry.ManagerAvailable = &available
		entry.UnavailableReason = pkg.UnavailableReason

		if tap := brew.TapNameFromQualifiedPackage(pkg.Name); tap != "" {
			tapEntry := ensureEntry(entries, canonicalIdentity("tap", tap), "tap", tap)
			tapEntry.MiseDesired = true
		}
	}
	for _, tap := range taps {
		entry := ensureEntry(entries, canonicalIdentity("tap", tap.Name), "tap", tap.Name)
		entry.MiseDesired = true
	}

	identities := make([]string, 0, len(entries))
	for identity := range entries {
		identities = append(identities, identity)
	}
	sort.Strings(identities)

	report := Report{
		SchemaVersion:       SchemaVersion,
		Status:              plan.StatusOK,
		Root:                root,
		BrewfilePath:        brewfilePath,
		MiseSources:         packageSet.Sources,
		Items:               make([]Item, 0, len(identities)),
		IgnoredMiseManagers: ignored,
	}
	for _, identity := range identities {
		item := *entries[identity]
		switch {
		case item.BrewfileDesired && item.MiseDesired:
			item.Parity = ParityMatch
			report.Summary.Matched++
		case item.BrewfileDesired:
			item.Parity = ParityBrewfileOnly
			report.Summary.BrewfileOnly++
			report.Status = plan.StatusDrift
		default:
			item.Parity = ParityMiseOnly
			report.Summary.MiseOnly++
			report.Status = plan.StatusDrift
		}
		report.Items = append(report.Items, item)
	}
	if len(report.IgnoredMiseManagers) == 0 {
		report.IgnoredMiseManagers = nil
	}
	return report, nil
}

func CanonicalBrewfileItem(item plan.Item) (identity string, kind string, name string, ok bool) {
	kind = ""
	switch item.Kind {
	case "brew":
		kind = "formula"
	case "cask", "tap":
		kind = item.Kind
	default:
		return "", "", "", false
	}
	name = brew.NormalizeDesiredName(item.Kind, item.Name)
	if name == "" {
		return "", "", "", false
	}
	return canonicalIdentity(kind, name), kind, name, true
}

func CanonicalHomebrewPackage(manager string, name string) (identity string, kind string, canonicalName string, ok bool) {
	brewKind := ""
	switch manager {
	case "brew":
		kind = "formula"
		brewKind = "brew"
	case "brew-cask":
		kind = "cask"
		brewKind = "cask"
	default:
		return "", "", "", false
	}
	canonicalName = brew.NormalizeDesiredName(brewKind, name)
	return canonicalIdentity(kind, canonicalName), kind, canonicalName, true
}

func canonicalIdentity(kind string, name string) string {
	return "brew/" + kind + "/" + name
}

func ensureEntry(entries map[string]*Item, identity string, kind string, name string) *Item {
	if entry, ok := entries[identity]; ok {
		return entry
	}
	entry := &Item{Identity: identity, Kind: kind, Name: name}
	entries[identity] = entry
	return entry
}
