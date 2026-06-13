package nativeaudit

import "testing"

func TestParseNativeAuditReports(t *testing.T) {
	tests := []struct {
		name  string
		parse func(string) bool
		raw   string
	}{
		{
			name: "npm",
			parse: func(raw string) bool {
				got, ok := ParseNPMReport(raw)
				return ok && got.Metadata.Vulnerabilities.High == 1 && len(got.Vulnerabilities) == 1
			},
			raw: `{"vulnerabilities":{"left-pad":{}},"metadata":{"vulnerabilities":{"high":1,"total":1}}}`,
		},
		{
			name: "generic",
			parse: func(raw string) bool {
				got, ok := ParseGenericReport(raw)
				return ok && got.Advisories == 2 && got.Vulnerable.Total == 2
			},
			raw: `{"advisories":{"a":{},"b":{}},"metadata":{"vulnerabilities":{"total":2}}}`,
		},
		{
			name: "cargo",
			parse: func(raw string) bool {
				got, ok := ParseCargoReport(raw)
				return ok && got.Vulnerabilities.Found && got.Vulnerabilities.Count == 1
			},
			raw: `{"vulnerabilities":{"found":true,"count":1,"list":[{}]}}`,
		},
		{
			name: "pip",
			parse: func(raw string) bool {
				got, ok := ParsePipReport(raw)
				return ok && len(got.Dependencies) == 1 && len(got.Dependencies[0].Vulns) == 1
			},
			raw: `{"dependencies":[{"name":"sample","version":"1.0.0","vulns":[{}]}]}`,
		},
		{
			name: "govulncheck",
			parse: func(raw string) bool {
				got, ok := ParseGovulncheckReport(raw)
				return ok && len(got.Findings) == 1 && got.Findings[0].OSV == "GO-1"
			},
			raw: "{\"finding\":{\"osv\":\"GO-1\",\"fixed_version\":\"v1.2.3\"}}\n{}",
		},
		{
			name: "dotnet",
			parse: func(raw string) bool {
				got, ok := ParseDotnetReport(raw)
				return ok && len(got.Projects) == 1 && len(got.Projects[0].Frameworks) == 1
			},
			raw: `{"projects":[{"frameworks":[{"topLevelPackages":[{"vulnerabilities":[{}]}]}]}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.parse(tt.raw) {
				t.Fatalf("expected %s report to parse", tt.name)
			}
		})
	}
}

func TestNativeAuditCountHelpers(t *testing.T) {
	composer, ok := ParseComposerReport(`{"advisories":{"pkg/a":[{},{}],"pkg/b":[{}]}}`)
	if !ok {
		t.Fatal("expected composer report to parse")
	}
	if got := CountComposerAdvisories(composer.Advisories); got != 3 {
		t.Fatalf("expected 3 composer advisories, got %d", got)
	}
	bundler, ok := ParseBundlerReport(`{"results":[{},{}],"vulnerabilities":[{}]}`)
	if !ok {
		t.Fatal("expected bundler report to parse")
	}
	if got := CountBundlerFindings(bundler); got != 2 {
		t.Fatalf("expected first populated bundler count, got %d", got)
	}
}

func TestParseNativeAuditReportsRejectInvalid(t *testing.T) {
	if _, ok := ParseNPMReport(""); ok {
		t.Fatal("expected empty npm report to be rejected")
	}
	if _, ok := ParseGovulncheckReport("not-json"); ok {
		t.Fatal("expected invalid govulncheck stream to be rejected")
	}
}
