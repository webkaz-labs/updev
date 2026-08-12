package packageexecutor

import (
	"fmt"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/packagemetadata"
	"github.com/webkaz-labs/updev/internal/packageparity"
	"github.com/webkaz-labs/updev/internal/plan"
)

const SchemaVersion = 1

const (
	ExecutorNative      = "native"
	ExecutorMise        = "mise"
	ExecutorUnsupported = "unsupported"
)

const (
	SourceBrewfile = "brewfile"
	SourceMise     = "mise"
	SourceBoth     = "both"
)

type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

type Capabilities struct {
	NativeProviders map[string]bool `json:"native_providers"`
}

type Input struct {
	Snapshot     packageparity.Snapshot
	Metadata     packagemetadata.Set
	Platform     Platform
	Capabilities Capabilities
}

type Report struct {
	SchemaVersion       int                          `json:"schema_version"`
	Status              plan.Status                  `json:"status"`
	Root                string                       `json:"root"`
	BrewfilePath        string                       `json:"brewfile_path"`
	PackageMetadataPath string                       `json:"package_metadata_path"`
	Platform            Platform                     `json:"platform"`
	Capabilities        Capabilities                 `json:"capabilities"`
	Summary             Summary                      `json:"summary"`
	Items               []Item                       `json:"items"`
	Diagnostics         []packagemetadata.Diagnostic `json:"diagnostics,omitempty"`
}

type Summary struct {
	Native      int `json:"native"`
	Mise        int `json:"mise"`
	Unsupported int `json:"unsupported"`
}

type Item struct {
	Identity             string      `json:"identity"`
	Provider             string      `json:"provider"`
	Kind                 string      `json:"kind"`
	Name                 string      `json:"name"`
	DesiredSource        string      `json:"desired_source"`
	Manager              string      `json:"manager,omitempty"`
	ManagerPackage       string      `json:"manager_package,omitempty"`
	ManagerAvailable     *bool       `json:"manager_available,omitempty"`
	NativeAvailable      bool        `json:"native_available"`
	MetadataExecutor     string      `json:"metadata_executor,omitempty"`
	IntentionalDuplicate bool        `json:"intentional_duplicate,omitempty"`
	Executor             string      `json:"executor"`
	Status               plan.Status `json:"status"`
	ReasonCode           string      `json:"reason_code"`
	Reason               string      `json:"reason"`
}

func DesiredIDs(snapshot packageparity.Snapshot) ([]string, error) {
	records, err := desiredRecords(snapshot)
	if err != nil {
		return nil, err
	}
	identities := make([]string, 0, len(records))
	for identity := range records {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	return identities, nil
}

func Build(input Input) (Report, error) {
	records, err := desiredRecords(input.Snapshot)
	if err != nil {
		return Report{}, err
	}
	metadata := map[string]packagemetadata.Package{}
	for _, pkg := range input.Metadata.Packages {
		metadata[pkg.Identity] = pkg
	}

	identities := make([]string, 0, len(records))
	for identity := range records {
		identities = append(identities, identity)
	}
	sort.Strings(identities)

	report := Report{
		SchemaVersion:       SchemaVersion,
		Status:              plan.StatusOK,
		Root:                input.Snapshot.Report.Root,
		BrewfilePath:        input.Snapshot.Report.BrewfilePath,
		PackageMetadataPath: input.Metadata.Path,
		Platform:            input.Platform,
		Capabilities:        normalizedCapabilities(input.Capabilities),
		Items:               make([]Item, 0, len(identities)),
		Diagnostics:         append([]packagemetadata.Diagnostic(nil), input.Metadata.Diagnostics...),
	}
	if len(report.Diagnostics) > 0 {
		report.Status = plan.StatusDrift
	}
	for _, identity := range identities {
		item := *records[identity]
		item.NativeAvailable = report.Capabilities.NativeProviders[item.Provider]
		if pkg, ok := metadata[identity]; ok {
			item.MetadataExecutor = pkg.Executor
			item.IntentionalDuplicate = pkg.IntentionalDuplicate != nil && *pkg.IntentionalDuplicate
		}
		selectExecutor(&item, input.Platform)
		switch item.Executor {
		case ExecutorNative:
			report.Summary.Native++
		case ExecutorMise:
			report.Summary.Mise++
		default:
			report.Summary.Unsupported++
		}
		if item.Status != plan.StatusOK {
			report.Status = plan.StatusDrift
		}
		report.Items = append(report.Items, item)
	}
	return report, nil
}

func desiredRecords(snapshot packageparity.Snapshot) (map[string]*Item, error) {
	records := map[string]*Item{}
	for _, parityItem := range snapshot.Report.Items {
		identity, err := packagemetadata.ParseIdentity(parityItem.Identity)
		if err != nil {
			return nil, err
		}
		source := SourceMise
		switch parityItem.Parity {
		case packageparity.ParityMatch:
			source = SourceBoth
		case packageparity.ParityBrewfileOnly:
			source = SourceBrewfile
		}
		records[parityItem.Identity] = &Item{
			Identity:         parityItem.Identity,
			Provider:         identity.Provider,
			Kind:             identity.Kind,
			Name:             identity.Name,
			DesiredSource:    source,
			ManagerAvailable: cloneBool(parityItem.ManagerAvailable),
		}
	}

	for _, pkg := range snapshot.PackageSet.Packages {
		identity, kind, name, ok := packageparity.CanonicalHomebrewPackage(pkg.Manager, pkg.Name)
		if !ok {
			var err error
			identity, kind, name, err = canonicalManagerPackage(pkg.Manager, pkg.Name)
			if err != nil {
				return nil, err
			}
		}
		record := records[identity]
		if record == nil {
			parsed, err := packagemetadata.ParseIdentity(identity)
			if err != nil {
				return nil, err
			}
			record = &Item{
				Identity:      identity,
				Provider:      parsed.Provider,
				Kind:          kind,
				Name:          name,
				DesiredSource: SourceMise,
			}
			records[identity] = record
		}
		if record.Manager != "" && (record.Manager != pkg.Manager || record.ManagerPackage != pkg.Name) {
			return nil, fmt.Errorf("multiple mise managers resolve to package identity %s", identity)
		}
		available := pkg.ManagerAvailable
		record.Manager = pkg.Manager
		record.ManagerPackage = pkg.Name
		record.ManagerAvailable = &available
	}

	for _, tap := range snapshot.Taps {
		identity := "brew/tap/" + tap.Name
		record := records[identity]
		if record == nil {
			record = &Item{
				Identity:      identity,
				Provider:      "brew",
				Kind:          "tap",
				Name:          tap.Name,
				DesiredSource: SourceMise,
			}
			records[identity] = record
		}
		record.Manager = "brew-tap"
		record.ManagerPackage = tap.Name
		available := false
		record.ManagerAvailable = &available
	}
	return records, nil
}

func canonicalManagerPackage(manager string, name string) (string, string, string, error) {
	provider := strings.TrimSpace(strings.ToLower(manager))
	canonicalName := strings.TrimSpace(name)
	kind := "package"
	switch provider {
	case "flatpak", "mas":
		kind = "app"
	}
	identity := provider + "/" + kind + "/" + canonicalName
	parsed, err := packagemetadata.ParseIdentity(identity)
	if err != nil {
		return "", "", "", fmt.Errorf("mise manager package identity: %w", err)
	}
	return parsed.String(), kind, canonicalName, nil
}

func normalizedCapabilities(capabilities Capabilities) Capabilities {
	result := Capabilities{NativeProviders: map[string]bool{}}
	for provider, available := range capabilities.NativeProviders {
		result.NativeProviders[provider] = available
	}
	return result
}

func selectExecutor(item *Item, platform Platform) {
	if item.DesiredSource == SourceBoth && !item.IntentionalDuplicate {
		unsupported(item, plan.StatusBlocked, "intentional-duplicate-required", "package is active in Brewfile and mise without an intentional duplicate annotation")
		return
	}
	miseAvailable := effectiveMiseAvailability(*item, platform)
	switch item.MetadataExecutor {
	case ExecutorNative:
		if item.NativeAvailable {
			ready(item, ExecutorNative, "explicit-native", "package metadata explicitly selects the available native provider")
		} else {
			unsupported(item, plan.StatusBlocked, "explicit-native-unavailable", "package metadata selects native, but the native provider is unavailable")
		}
		return
	case ExecutorMise:
		if item.DesiredSource != SourceBrewfile && miseAvailable {
			ready(item, ExecutorMise, "explicit-mise", "package metadata explicitly selects the available mise manager")
		} else {
			unsupported(item, plan.StatusBlocked, "explicit-mise-unavailable", "package metadata selects mise, but the item is not mise-declared or the manager is unavailable")
		}
		return
	}
	if item.DesiredSource == SourceBrewfile {
		if item.NativeAvailable {
			ready(item, ExecutorNative, "brewfile-native-authority", "Brewfile-only desired state stays on the native item-scoped provider")
		} else {
			unsupported(item, plan.StatusUnavailable, "native-provider-unavailable", "Brewfile-only desired state has no available native provider")
		}
		return
	}
	if isMacOSX64Homebrew(*item, platform) {
		if item.NativeAvailable {
			ready(item, ExecutorNative, "macos-x64-native-homebrew", "macOS x64 Homebrew packages use the native item-scoped provider")
		} else {
			unsupported(item, plan.StatusUnavailable, "native-provider-unavailable", "macOS x64 requires native Homebrew, but it is unavailable")
		}
		return
	}
	if miseAvailable {
		ready(item, ExecutorMise, "mise-manager-available", "mise desired state and manager capability are available for this platform")
		return
	}
	if item.NativeAvailable {
		ready(item, ExecutorNative, "native-provider-fallback", "mise manager capability is unavailable; an explicit native provider adapter is available")
		return
	}
	unsupported(item, plan.StatusUnavailable, "unsupported-executor", "no supported mise manager or native provider adapter is available")
}

func effectiveMiseAvailability(item Item, platform Platform) bool {
	if item.ManagerAvailable == nil || !*item.ManagerAvailable {
		return false
	}
	if !supportedPlatform(platform) {
		return false
	}
	return !isMacOSX64Homebrew(item, platform)
}

func supportedPlatform(platform Platform) bool {
	switch platform.OS {
	case "darwin", "linux":
		return platform.Arch == "amd64" || platform.Arch == "arm64"
	default:
		return false
	}
}

func isMacOSX64Homebrew(item Item, platform Platform) bool {
	return platform.OS == "darwin" && platform.Arch == "amd64" && item.Provider == "brew"
}

func ready(item *Item, executor string, reasonCode string, reason string) {
	item.Executor = executor
	item.Status = plan.StatusOK
	item.ReasonCode = reasonCode
	item.Reason = reason
}

func unsupported(item *Item, status plan.Status, reasonCode string, reason string) {
	item.Executor = ExecutorUnsupported
	item.Status = status
	item.ReasonCode = reasonCode
	item.Reason = reason
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
