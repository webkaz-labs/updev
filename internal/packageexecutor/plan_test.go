package packageexecutor

import (
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/packagemetadata"
	"github.com/webkaz-labs/updev/internal/packageparity"
	"github.com/webkaz-labs/updev/internal/plan"
)

func TestBuildSelectsNativeHomebrewOnMacOSX64(t *testing.T) {
	snapshot := packageSnapshot(
		[]packageparity.Item{
			parityItem("brew/formula/jq", "formula", "jq", packageparity.ParityMatch),
			parityItem("brew/cask/firefox", "cask", "firefox", packageparity.ParityMatch),
			parityItem("brew/tap/example/tools", "tap", "example/tools", packageparity.ParityMatch),
		},
		[]mise.BootstrapPackageDesired{
			{Identity: "brew:jq", Manager: "brew", Name: "jq", RequestedVersion: "latest", ManagerAvailable: false},
			{Identity: "brew-cask:firefox", Manager: "brew-cask", Name: "firefox", RequestedVersion: "latest", ManagerAvailable: true},
		},
		[]mise.BootstrapTapDesired{{Identity: "brew-tap:example/tools", Name: "example/tools"}},
	)
	metadata := metadataFor(t, snapshot, `
[packages."brew/formula/jq"]
intentional_duplicate = true
[packages."brew/cask/firefox"]
intentional_duplicate = true
[packages."brew/tap/example/tools"]
intentional_duplicate = true
`)
	report, err := Build(Input{
		Snapshot:     snapshot,
		Metadata:     metadata,
		Platform:     Platform{OS: "darwin", Arch: "amd64"},
		Capabilities: Capabilities{NativeProviders: map[string]bool{"brew": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != plan.StatusOK || report.Summary.Native != 3 || report.Summary.Mise != 0 || report.Summary.Unsupported != 0 {
		t.Fatalf("expected native macOS x64 plan, got %#v", report)
	}
	for _, item := range report.Items {
		if item.Executor != ExecutorNative || item.ReasonCode != "macos-x64-native-homebrew" {
			t.Fatalf("expected native x64 item, got %#v", item)
		}
	}
}

func TestBuildUsesMiseOnMacOSArm64AndKeepsBrewfileOnlyNative(t *testing.T) {
	snapshot := packageSnapshot(
		[]packageparity.Item{
			parityItem("brew/formula/jq", "formula", "jq", packageparity.ParityMatch),
			parityItem("brew/formula/git", "formula", "git", packageparity.ParityBrewfileOnly),
			parityItem("brew/cask/firefox", "cask", "firefox", packageparity.ParityMatch),
			parityItem("brew/tap/example/tools", "tap", "example/tools", packageparity.ParityMatch),
		},
		[]mise.BootstrapPackageDesired{
			{Identity: "brew:jq", Manager: "brew", Name: "jq", RequestedVersion: "latest", ManagerAvailable: true},
			{Identity: "brew-cask:firefox", Manager: "brew-cask", Name: "firefox", RequestedVersion: "latest", ManagerAvailable: true},
		},
		[]mise.BootstrapTapDesired{{Identity: "brew-tap:example/tools", Name: "example/tools"}},
	)
	metadata := metadataFor(t, snapshot, `
[packages."brew/formula/jq"]
intentional_duplicate = true
[packages."brew/cask/firefox"]
intentional_duplicate = true
executor = "native"
[packages."brew/tap/example/tools"]
intentional_duplicate = true
`)
	report, err := Build(Input{
		Snapshot:     snapshot,
		Metadata:     metadata,
		Platform:     Platform{OS: "darwin", Arch: "arm64"},
		Capabilities: Capabilities{NativeProviders: map[string]bool{"brew": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := itemExecutors(report.Items)
	if got["brew/formula/jq"] != ExecutorMise || got["brew/formula/git"] != ExecutorNative || got["brew/cask/firefox"] != ExecutorNative || got["brew/tap/example/tools"] != ExecutorNative {
		t.Fatalf("unexpected arm64 executor plan: %#v", report.Items)
	}
	if itemByIdentity(report.Items, "brew/formula/jq").ManagerPackage != "jq" {
		t.Fatalf("expected original mise manager package identity, got %#v", report.Items)
	}
}

func TestBuildSelectsAvailableMiseManagersOnLinux(t *testing.T) {
	snapshot := packageSnapshot(nil, []mise.BootstrapPackageDesired{
		{Identity: "apt:curl", Manager: "apt", Name: "curl", RequestedVersion: "latest", ManagerAvailable: true},
		{Identity: "flatpak:org.mozilla.firefox", Manager: "flatpak", Name: "org.mozilla.firefox", RequestedVersion: "latest", ManagerAvailable: true},
		{Identity: "dnf:jq", Manager: "dnf", Name: "jq", RequestedVersion: "latest", ManagerAvailable: false},
	}, nil)
	metadata := metadataFor(t, snapshot, "")
	report, err := Build(Input{
		Snapshot:     snapshot,
		Metadata:     metadata,
		Platform:     Platform{OS: "linux", Arch: "arm64"},
		Capabilities: Capabilities{NativeProviders: map[string]bool{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := itemExecutors(report.Items)
	if got["apt/package/curl"] != ExecutorMise || got["flatpak/app/org.mozilla.firefox"] != ExecutorMise || got["dnf/package/jq"] != ExecutorUnsupported {
		t.Fatalf("unexpected Linux executor plan: %#v", report.Items)
	}
	if report.Status != plan.StatusDrift || report.Summary.Mise != 2 || report.Summary.Unsupported != 1 {
		t.Fatalf("unexpected Linux summary: %#v", report)
	}
}

func TestBuildFailsClosedForDuplicateAndUnavailableExplicitExecutor(t *testing.T) {
	snapshot := packageSnapshot(
		[]packageparity.Item{
			parityItem("brew/formula/jq", "formula", "jq", packageparity.ParityMatch),
			parityItem("brew/formula/git", "formula", "git", packageparity.ParityBrewfileOnly),
		},
		[]mise.BootstrapPackageDesired{{Identity: "brew:jq", Manager: "brew", Name: "jq", RequestedVersion: "latest", ManagerAvailable: true}},
		nil,
	)
	metadata := metadataFor(t, snapshot, `
[packages."brew/formula/git"]
executor = "mise"
`)
	report, err := Build(Input{
		Snapshot:     snapshot,
		Metadata:     metadata,
		Platform:     Platform{OS: "darwin", Arch: "arm64"},
		Capabilities: Capabilities{NativeProviders: map[string]bool{"brew": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, item := range report.Items {
		reasons[item.Identity] = item.ReasonCode
	}
	if reasons["brew/formula/jq"] != "intentional-duplicate-required" || reasons["brew/formula/git"] != "explicit-mise-unavailable" {
		t.Fatalf("expected fail-closed reasons, got %#v", report.Items)
	}
}

func TestBuildFailsClosedForUnavailableNativeAndUnsupportedPlatform(t *testing.T) {
	tests := []struct {
		name         string
		platform     Platform
		metadata     string
		capabilities Capabilities
		reasonCode   string
	}{
		{
			name:         "explicit native provider is unavailable",
			platform:     Platform{OS: "linux", Arch: "amd64"},
			metadata:     "executor = \"native\"\n",
			capabilities: Capabilities{NativeProviders: map[string]bool{}},
			reasonCode:   "explicit-native-unavailable",
		},
		{
			name:         "mise manager is not promoted on Windows",
			platform:     Platform{OS: "windows", Arch: "amd64"},
			capabilities: Capabilities{NativeProviders: map[string]bool{}},
			reasonCode:   "unsupported-executor",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot := packageSnapshot(nil, []mise.BootstrapPackageDesired{{
				Identity: "apt:curl", Manager: "apt", Name: "curl", RequestedVersion: "latest", ManagerAvailable: true,
			}}, nil)
			metadataText := ""
			if tt.metadata != "" {
				metadataText = "[packages.\"apt/package/curl\"]\n" + tt.metadata
			}
			report, err := Build(Input{
				Snapshot:     snapshot,
				Metadata:     metadataFor(t, snapshot, metadataText),
				Platform:     tt.platform,
				Capabilities: tt.capabilities,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Items) != 1 || report.Items[0].Executor != ExecutorUnsupported || report.Items[0].ReasonCode != tt.reasonCode {
				t.Fatalf("expected fail-closed executor with reason %q, got %#v", tt.reasonCode, report.Items)
			}
			if report.Status != plan.StatusDrift || report.Summary.Unsupported != 1 {
				t.Fatalf("expected unsupported drift summary, got %#v", report)
			}
		})
	}
}

func TestDetailRowsExposeCompactExecutorEvidence(t *testing.T) {
	report := Report{Items: []Item{{
		Identity:      "brew/formula/jq",
		Kind:          "formula",
		Name:          "jq",
		DesiredSource: SourceBrewfile,
		Executor:      ExecutorNative,
		Status:        plan.StatusOK,
		ReasonCode:    "brewfile-native-authority",
		Reason:        "Brewfile-only desired state stays on the native item-scoped provider",
	}}}
	rows := DetailRows(report, "ja")
	if len(rows) != 1 || !strings.Contains(rows[0].Summary, "native") || !strings.Contains(rows[0].Detail, "Brewfile") || len(rows[0].Columns) != 5 {
		t.Fatalf("unexpected executor detail row: %#v", rows)
	}
}

func packageSnapshot(parityItems []packageparity.Item, packages []mise.BootstrapPackageDesired, taps []mise.BootstrapTapDesired) packageparity.Snapshot {
	return packageparity.Snapshot{
		Report:     packageparity.Report{Root: "/repo", BrewfilePath: "/repo/Brewfile", Items: parityItems},
		PackageSet: mise.BootstrapPackageSet{Sources: []mise.ConfigSource{{Path: "/config/mise.toml", ReportedOrder: 1}}, Packages: packages},
		Taps:       taps,
	}
}

func parityItem(identity string, kind string, name string, parity string) packageparity.Item {
	return packageparity.Item{Identity: identity, Kind: kind, Name: name, Parity: parity, BrewfileDesired: parity != packageparity.ParityMiseOnly, MiseDesired: parity != packageparity.ParityBrewfileOnly}
}

func metadataFor(t *testing.T, snapshot packageparity.Snapshot, packageTables string) packagemetadata.Set {
	t.Helper()
	identities, err := DesiredIDs(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	set, err := packagemetadata.Parse("/config/package-metadata.toml", []byte("schema_version = 1\n"+packageTables), identities)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func itemExecutors(items []Item) map[string]string {
	result := map[string]string{}
	for _, item := range items {
		result[item.Identity] = item.Executor
	}
	return result
}

func itemByIdentity(items []Item, identity string) Item {
	for _, item := range items {
		if item.Identity == identity {
			return item
		}
	}
	return Item{}
}
