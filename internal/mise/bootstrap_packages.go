package mise

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/webkaz-labs/updev/internal/runner"

	"github.com/BurntSushi/toml"
)

type BootstrapPackageSet struct {
	Sources  []ConfigSource            `json:"sources"`
	Packages []BootstrapPackageDesired `json:"packages"`
}

type BootstrapPackageDesired struct {
	Identity          string `json:"identity"`
	Manager           string `json:"manager"`
	Name              string `json:"name"`
	RequestedVersion  string `json:"requested_version"`
	State             string `json:"state"`
	InstalledVersion  string `json:"installed_version,omitempty"`
	ManagerAvailable  bool   `json:"manager_available"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`
}

type bootstrapPackageManagerStatus struct {
	Available *bool                       `json:"available"`
	Reason    string                      `json:"reason"`
	Packages  []bootstrapPackageStatusRow `json:"packages"`
}

type bootstrapPackageStatusRow struct {
	Package          string `json:"package"`
	RequestedVersion string `json:"requested_version"`
	State            string `json:"state"`
	InstalledVersion string `json:"installed_version"`
}

type bootstrapDesiredDocument struct {
	Packages map[string]string `toml:"packages"`
	Brew     struct {
		Taps map[string]string `toml:"taps"`
	} `toml:"brew"`
}

// ReadBootstrapDesiredState reads mise's resolved bootstrap declaration without
// inspecting live package-manager state. Callers that need availability or
// installed versions must use ReadBootstrapPackageSet instead.
func ReadBootstrapDesiredState(ctx context.Context, commandRunner runner.Runner, root string) (BootstrapPackageSet, []BootstrapTapDesired, error) {
	if _, err := commandRunner.LookPath("mise"); err != nil {
		return BootstrapPackageSet{}, nil, nil
	}
	result := commandRunner.Run(ctx, "mise", "config", "get", "bootstrap", "--cd", root)
	if result.Err != nil {
		if strings.Contains(result.Stderr, "Key not found: bootstrap") {
			return BootstrapPackageSet{}, nil, nil
		}
		return BootstrapPackageSet{}, nil, fmt.Errorf("mise config get bootstrap: %s", runner.ResultDetail(result, "command failed", runner.ResultDetailOption{}))
	}
	packages, taps, err := BootstrapDesiredFromConfigTOML([]byte(result.Stdout))
	if err != nil {
		return BootstrapPackageSet{}, nil, err
	}
	return BootstrapPackageSet{Packages: packages}, taps, nil
}

func BootstrapDesiredFromConfigTOML(data []byte) ([]BootstrapPackageDesired, []BootstrapTapDesired, error) {
	var document bootstrapDesiredDocument
	if _, err := toml.Decode(string(data), &document); err != nil {
		return nil, nil, fmt.Errorf("parse resolved mise bootstrap config: %w", err)
	}

	identities := make([]string, 0, len(document.Packages))
	for identity := range document.Packages {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	packages := make([]BootstrapPackageDesired, 0, len(identities))
	for _, identity := range identities {
		manager, name, ok := strings.Cut(identity, ":")
		manager = strings.TrimSpace(manager)
		name = strings.TrimSpace(name)
		version := strings.TrimSpace(document.Packages[identity])
		if !ok || manager == "" || name == "" || identity != manager+":"+name {
			return nil, nil, fmt.Errorf("resolved mise bootstrap package identity %q is invalid", identity)
		}
		if version == "" {
			return nil, nil, fmt.Errorf("resolved mise bootstrap package %s is missing version", identity)
		}
		packages = append(packages, BootstrapPackageDesired{
			Identity: identity, Manager: manager, Name: name, RequestedVersion: version,
		})
	}

	names := make([]string, 0, len(document.Brew.Taps))
	for name := range document.Brew.Taps {
		names = append(names, name)
	}
	sort.Strings(names)
	taps := make([]BootstrapTapDesired, 0, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) != name || name == "" || strings.IndexFunc(name, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r)
		}) >= 0 {
			return nil, nil, fmt.Errorf("resolved mise bootstrap tap name %q is invalid", name)
		}
		taps = append(taps, BootstrapTapDesired{
			Identity: "brew-tap:" + name,
			Name:     name,
			URL:      strings.TrimSpace(document.Brew.Taps[name]),
		})
	}
	return packages, taps, nil
}

func ReadBootstrapPackageSet(ctx context.Context, commandRunner runner.Runner, root string) (BootstrapPackageSet, error) {
	sources, err := ConfigSources(ctx, commandRunner, root)
	if err != nil {
		return BootstrapPackageSet{}, err
	}
	result := commandRunner.Run(ctx, "mise", "bootstrap", "status", "--json", "--cd", root)
	if result.Err != nil {
		return BootstrapPackageSet{}, fmt.Errorf("mise bootstrap status --json: %s", runner.ResultDetail(result, "command failed", runner.ResultDetailOption{}))
	}
	packages, err := BootstrapPackagesFromStatusJSON([]byte(result.Stdout))
	if err != nil {
		return BootstrapPackageSet{}, err
	}
	return BootstrapPackageSet{Sources: sources, Packages: packages}, nil
}

func BootstrapPackagesFromStatusJSON(data []byte) ([]BootstrapPackageDesired, error) {
	var payload struct {
		Packages map[string]bootstrapPackageManagerStatus `json:"packages"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse mise bootstrap package status: %w", err)
	}
	if payload.Packages == nil {
		return nil, fmt.Errorf("mise bootstrap status is missing packages object")
	}

	managers := make([]string, 0, len(payload.Packages))
	for manager := range payload.Packages {
		managers = append(managers, manager)
	}
	sort.Strings(managers)

	seen := map[string]bool{}
	packages := []BootstrapPackageDesired{}
	for _, rawManager := range managers {
		manager := strings.TrimSpace(rawManager)
		status := payload.Packages[rawManager]
		if manager == "" {
			return nil, fmt.Errorf("mise bootstrap package manager is empty")
		}
		if status.Available == nil {
			return nil, fmt.Errorf("mise bootstrap package manager %q is missing available", manager)
		}
		rows := append([]bootstrapPackageStatusRow{}, status.Packages...)
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Package != rows[j].Package {
				return rows[i].Package < rows[j].Package
			}
			return rows[i].RequestedVersion < rows[j].RequestedVersion
		})
		for _, row := range rows {
			name := strings.TrimSpace(row.Package)
			if name == "" {
				return nil, fmt.Errorf("mise bootstrap package name is empty for manager %q", manager)
			}
			identity := manager + ":" + name
			if seen[identity] {
				return nil, fmt.Errorf("duplicate mise bootstrap package identity: %s", identity)
			}
			seen[identity] = true
			requestedVersion := strings.TrimSpace(row.RequestedVersion)
			if requestedVersion == "" {
				return nil, fmt.Errorf("mise bootstrap package %s is missing requested_version", identity)
			}
			state := strings.TrimSpace(row.State)
			if state == "" {
				return nil, fmt.Errorf("mise bootstrap package %s is missing state", identity)
			}
			packages = append(packages, BootstrapPackageDesired{
				Identity:          identity,
				Manager:           manager,
				Name:              name,
				RequestedVersion:  requestedVersion,
				State:             state,
				InstalledVersion:  strings.TrimSpace(row.InstalledVersion),
				ManagerAvailable:  *status.Available,
				UnavailableReason: strings.TrimSpace(status.Reason),
			})
		}
	}
	return packages, nil
}
