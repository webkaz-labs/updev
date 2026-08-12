package mise

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/webkaz-labs/updev/internal/runner"
)

func TestReadBootstrapPackageSetNormalizesStandaloneConfig(t *testing.T) {
	root := "/standalone/project"
	fake := fakeRunner{results: map[string]runner.Result{
		"mise\x00config\x00ls\x00--json\x00--cd\x00" + root: {Stdout: `[
			{"path":"/standalone/project/mise.toml","tools":[]},
			{"path":"","tools":[]}
		]`},
		"mise\x00bootstrap\x00status\x00--json\x00--cd\x00" + root: {Stdout: `{
			"packages": {
				"brew-cask": {"available":true,"packages":[{"package":"firefox","requested_version":"latest","state":"missing","installed_version":""}]},
				"apt": {"available":false,"reason":"only available on linux","packages":[{"package":"curl","requested_version":"8.5.0-2","state":"skipped"}]},
				"brew": {"available":false,"reason":"unsupported architecture","packages":[{"package":"jq","requested_version":"latest","state":"skipped"}]}
			},
			"tools": []
		}`},
	}}

	set, err := ReadBootstrapPackageSet(context.Background(), &fake, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Sources) != 1 || set.Sources[0].Path != "/standalone/project/mise.toml" {
		t.Fatalf("expected standalone source without empty sentinel, got %#v", set.Sources)
	}
	identities := []string{}
	for _, pkg := range set.Packages {
		identities = append(identities, pkg.Identity)
	}
	if strings.Join(identities, ",") != "apt:curl,brew:jq,brew-cask:firefox" {
		t.Fatalf("unexpected deterministic package order: %#v", set.Packages)
	}
	if set.Packages[0].ManagerAvailable || set.Packages[0].UnavailableReason != "only available on linux" {
		t.Fatalf("expected unavailable manager desired record, got %#v", set.Packages[0])
	}
	if set.Packages[2].State != "missing" || !set.Packages[2].ManagerAvailable {
		t.Fatalf("expected available missing cask, got %#v", set.Packages[2])
	}
	if len(fake.calls) != 2 {
		t.Fatalf("expected config and aggregate status reads, got %#v", fake.calls)
	}

	encoded, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"sources":[{"path":"/standalone/project/mise.toml","reported_order":1,"tools":[]}],"packages":[{"identity":"apt:curl","manager":"apt","name":"curl","requested_version":"8.5.0-2","state":"skipped","manager_available":false,"unavailable_reason":"only available on linux"},{"identity":"brew:jq","manager":"brew","name":"jq","requested_version":"latest","state":"skipped","manager_available":false,"unavailable_reason":"unsupported architecture"},{"identity":"brew-cask:firefox","manager":"brew-cask","name":"firefox","requested_version":"latest","state":"missing","manager_available":true}]}`
	if string(encoded) != want {
		t.Fatalf("unexpected deterministic JSON:\n%s\nwant:\n%s", encoded, want)
	}
}

func TestBootstrapPackagesFromStatusJSONRejectsInvalidContracts(t *testing.T) {
	for name, input := range map[string]string{
		"missing packages":  `{"tools":[]}`,
		"wrong root shape":  `{"packages":[]}`,
		"missing available": `{"packages":{"brew":{"packages":[]}}}`,
		"empty package":     `{"packages":{"brew":{"available":true,"packages":[{"package":"","requested_version":"latest","state":"missing"}]}}}`,
		"missing version":   `{"packages":{"brew":{"available":true,"packages":[{"package":"jq","state":"missing"}]}}}`,
		"missing state":     `{"packages":{"brew":{"available":true,"packages":[{"package":"jq","requested_version":"latest"}]}}}`,
		"duplicate identity": `{"packages":{"brew":{"available":true,"packages":[
			{"package":"jq","requested_version":"latest","state":"missing"},
			{"package":"jq","requested_version":"1.8","state":"installed"}
		]}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BootstrapPackagesFromStatusJSON([]byte(input)); err == nil {
				t.Fatalf("expected package contract error for %s", input)
			}
		})
	}
}

func TestBootstrapDesiredFromConfigTOML(t *testing.T) {
	packages, taps, err := BootstrapDesiredFromConfigTOML([]byte(`[packages]
"brew:homebrew/core/btop" = "latest"
"brew-cask:example/tools/app" = "latest"
"apt:curl" = "8.5.0-2"

[brew.taps]
"example/tools" = "https://example.invalid/tools"
`))
	if err != nil {
		t.Fatal(err)
	}
	gotPackages := make([]string, 0, len(packages))
	for _, pkg := range packages {
		gotPackages = append(gotPackages, pkg.Identity+"@"+pkg.RequestedVersion)
	}
	if strings.Join(gotPackages, ",") != "apt:curl@8.5.0-2,brew-cask:example/tools/app@latest,brew:homebrew/core/btop@latest" {
		t.Fatalf("unexpected packages: %v", gotPackages)
	}
	if len(taps) != 1 || taps[0].Identity != "brew-tap:example/tools" || taps[0].URL != "https://example.invalid/tools" {
		t.Fatalf("unexpected taps: %#v", taps)
	}
}

func TestReadBootstrapDesiredStateTreatsMissingBootstrapAsEmpty(t *testing.T) {
	fake := fakeRunner{results: map[string]runner.Result{
		"mise\x00config\x00get\x00bootstrap\x00--cd\x00/repo": {
			Err: os.ErrNotExist, Code: 1, Stderr: "mise ERROR Key not found: bootstrap in /tmp/mise.toml",
		},
	}}
	set, taps, err := ReadBootstrapDesiredState(context.Background(), &fake, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Packages) != 0 || len(taps) != 0 {
		t.Fatalf("expected empty desired state, got set=%#v taps=%#v", set, taps)
	}
	if len(fake.calls) != 1 || strings.Join(fake.calls[0], "\x00") != "mise\x00config\x00get\x00bootstrap\x00--cd\x00/repo" {
		t.Fatalf("unexpected calls: %v", fake.calls)
	}
}

func TestReadBootstrapDesiredStateTreatsUnavailableMiseAsEmpty(t *testing.T) {
	set, taps, err := ReadBootstrapDesiredState(context.Background(), unavailableBootstrapRunner{}, "/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Packages) != 0 || len(taps) != 0 {
		t.Fatalf("expected empty desired state without mise, got set=%#v taps=%#v", set, taps)
	}
}

type unavailableBootstrapRunner struct{}

func (unavailableBootstrapRunner) LookPath(string) (string, error) { return "", os.ErrNotExist }

func (unavailableBootstrapRunner) Run(context.Context, string, ...string) runner.Result {
	panic("Run must not be called when mise is unavailable")
}
