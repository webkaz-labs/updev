package securityscanner

import "testing"

func TestParsePURL(t *testing.T) {
	tests := []struct {
		name      string
		purl      string
		ecosystem string
		pkg       string
		version   string
	}{
		{name: "npm", purl: "pkg:npm/left-pad@1.0.0", ecosystem: "npm", pkg: "left-pad", version: "1.0.0"},
		{name: "npm scoped", purl: "pkg:npm/%40scope/name@2.0.0", ecosystem: "npm", pkg: "@scope/name", version: "2.0.0"},
		{name: "cargo", purl: "pkg:cargo/ripgrep@14.1.1", ecosystem: "crates.io", pkg: "ripgrep", version: "14.1.1"},
		{name: "maven", purl: "pkg:maven/org.apache.commons/commons-lang3@3.14.0", ecosystem: "Maven", pkg: "org.apache.commons:commons-lang3", version: "3.14.0"},
		{name: "composer", purl: "pkg:composer/symfony/console@7.0.0", ecosystem: "Packagist", pkg: "symfony/console", version: "7.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePURL(tt.purl)
			if got.Ecosystem != tt.ecosystem || got.Name != tt.pkg || got.Version != tt.version {
				t.Fatalf("unexpected parsed PURL: %#v", got)
			}
		})
	}
}

func TestPackageInfoFromPURLUsesFallbacks(t *testing.T) {
	got := PackageInfoFromPURL("", "left-pad", "1.0.0", "node-pkg")
	if got.Name != "left-pad" || got.Version != "1.0.0" || got.Ecosystem != "npm" {
		t.Fatalf("unexpected fallback package info: %#v", got)
	}
}
