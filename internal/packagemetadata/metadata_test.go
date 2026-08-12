package packagemetadata

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseLoadsSortedMetadataAndStaleDiagnostics(t *testing.T) {
	data := []byte(`schema_version = 1

[packages."mise/tool/github:owner/repository"]
executor = "mise"

[packages."apt/package/curl"]
lifecycle = "base-system"

[packages."brew/formula/git"]
reason = "keep system git available"
lifecycle = "system-fallback"
executor = "native"
intentional_duplicate = true

[packages."brew/formula/git".homebrew]
link = false
`)
	set, err := Parse("/config/package-metadata.toml", data, []string{"brew/formula/git"})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Packages) != 3 || set.Packages[0].Identity != "apt/package/curl" || set.Packages[1].Identity != "brew/formula/git" || set.Packages[2].Identity != "mise/tool/github:owner/repository" {
		t.Fatalf("unexpected packages: %#v", set.Packages)
	}
	git := set.Packages[1]
	if git.IntentionalDuplicate == nil || !*git.IntentionalDuplicate || git.Homebrew == nil || git.Homebrew.Link == nil || *git.Homebrew.Link {
		t.Fatalf("expected optional metadata to remain explicit: %#v", git)
	}
	if len(set.Diagnostics) != 2 ||
		set.Diagnostics[0].Code != DiagnosticStalePackageMetadata || set.Diagnostics[0].Identity != "apt/package/curl" ||
		set.Diagnostics[1].Code != DiagnosticStalePackageMetadata || set.Diagnostics[1].Identity != "mise/tool/github:owner/repository" {
		t.Fatalf("unexpected diagnostics: %#v", set.Diagnostics)
	}

	encoded, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"path":"/config/package-metadata.toml"`) {
		t.Fatalf("expected deterministic path in JSON: %s", encoded)
	}
}

func TestParseRejectsInvalidSchema(t *testing.T) {
	for _, data := range []string{
		`[packages."brew/formula/git"]
reason = "required schema"
`,
		`schema_version = 2
[packages."brew/formula/git"]
reason = "unsupported schema"
`,
	} {
		if _, err := Parse("metadata.toml", []byte(data), nil); err == nil || !strings.Contains(err.Error(), "schema_version = 1") {
			t.Fatalf("expected schema error, got %v", err)
		}
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	cases := []string{
		"unknown = true\nschema_version = 1\n",
		"schema_version = 1\n[packages.\"brew/formula/git\"]\nreason = \"x\"\nversion = \"2\"\n",
		"schema_version = 1\n[packages.\"brew/formula/git\"]\nreason = \"x\"\n[packages.\"brew/formula/git\".apt]\nlink = false\n",
		"schema_version = 1\n[packages.\"brew/formula/git\"]\nreason = \"x\"\n[packages.\"brew/formula/git\".homebrew]\nlink = false\nforce = true\n",
	}
	for _, data := range cases {
		if _, err := Parse("metadata.toml", []byte(data), nil); err == nil || !strings.Contains(err.Error(), "unknown keys") {
			t.Fatalf("expected unknown-key error for %q, got %v", data, err)
		}
	}
}

func TestParseRejectsDuplicatePackageTable(t *testing.T) {
	data := []byte(`schema_version = 1
[packages."brew/formula/git"]
reason = "one"
[packages."brew/formula/git"]
reason = "two"
`)
	if _, err := Parse("metadata.toml", data, nil); err == nil || !strings.Contains(strings.ToLower(err.Error()), "already been defined") {
		t.Fatalf("expected duplicate table error, got %v", err)
	}
}

func TestParseRejectsInvalidIdentityAndEmptyMetadata(t *testing.T) {
	cases := []struct {
		name string
		data string
		want string
	}{
		{"missing kind", "schema_version = 1\n[packages.\"brew/git\"]\nreason = \"x\"\n", "provider/kind/name"},
		{"uppercase provider", "schema_version = 1\n[packages.\"Brew/formula/git\"]\nreason = \"x\"\n", "invalid provider"},
		{"space in name", "schema_version = 1\n[packages.\"brew/formula/git cli\"]\nreason = \"x\"\n", "whitespace"},
		{"empty package", "schema_version = 1\n[packages.\"brew/formula/git\"]\n", "has no metadata"},
		{"empty homebrew", "schema_version = 1\n[packages.\"brew/formula/git\".homebrew]\n", "empty homebrew"},
		{"invalid executor", "schema_version = 1\n[packages.\"brew/formula/git\"]\nexecutor = \"bundle\"\n", "invalid executor"},
		{"blank reason", "schema_version = 1\n[packages.\"brew/formula/git\"]\nreason = \"  \"\n", "must be non-empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse("metadata.toml", []byte(tc.data), nil); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestParsePreservesOpaqueName(t *testing.T) {
	identity := "mise/npm/@scope/tool/subpath"
	parsed, err := ParseIdentity(identity)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Provider != "mise" || parsed.Kind != "npm" || parsed.Name != "@scope/tool/subpath" || parsed.String() != identity {
		t.Fatalf("unexpected identity: %#v", parsed)
	}
}

func TestParseRejectsDuplicateDesiredIdentity(t *testing.T) {
	data := []byte("schema_version = 1\n")
	if _, err := Parse("metadata.toml", data, []string{"brew/formula/git", "brew/formula/git"}); err == nil || !strings.Contains(err.Error(), "duplicate desired") {
		t.Fatalf("expected duplicate desired identity error, got %v", err)
	}
}

func TestLoadMissingFileReturnsEmptySet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "package-metadata.toml")
	set, err := Load(path, []string{"brew/formula/git"})
	if err != nil {
		t.Fatal(err)
	}
	if set.Path != path || len(set.Packages) != 0 || len(set.Diagnostics) != 0 {
		t.Fatalf("expected empty optional metadata set, got %#v", set)
	}
}

func TestLoadStandaloneFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "updev", "package-metadata.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("schema_version = 1\n[packages.\"apt/package/curl\"]\nexecutor = \"native\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	set, err := Load(path, []string{"apt/package/curl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Packages) != 1 || len(set.Diagnostics) != 0 {
		t.Fatalf("unexpected standalone set: %#v", set)
	}
}
