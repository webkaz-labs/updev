package securityscanner

import "testing"

func TestParseScannerReports(t *testing.T) {
	tests := []struct {
		name  string
		parse func(string) bool
		raw   string
	}{
		{
			name: "osv",
			parse: func(raw string) bool {
				got, ok := ParseOSVReport(raw)
				return ok && len(got.Results) == 1 && got.Results[0].Packages[0].Package.Name == "left-pad"
			},
			raw: `{"results":[{"source":{"path":"package-lock.json","type":"lockfile"},"packages":[{"package":{"name":"left-pad","version":"1.0.0","ecosystem":"npm"},"vulnerabilities":[{"id":"OSV-1"}]}]}]}`,
		},
		{
			name: "gitleaks",
			parse: func(raw string) bool {
				got, ok := ParseGitleaksReport(raw)
				return ok && len(got) == 1 && got[0].RuleID == "generic-api-key"
			},
			raw: `[{"Description":"API key","File":"config.env","StartLine":1,"EndLine":1,"RuleID":"generic-api-key"}]`,
		},
		{
			name: "zizmor",
			parse: func(raw string) bool {
				got, ok := ParseZizmorReport(raw)
				return ok && len(got) == 1 && got[0].Ident == "github-env"
			},
			raw: `[{"ident":"github-env","desc":"unsafe env","determinations":{"confidence":"high","severity":"medium"}}]`,
		},
		{
			name: "trivy",
			parse: func(raw string) bool {
				got, ok := ParseTrivyReport(raw)
				return ok && len(got.Results) == 1 && got.Results[0].Vulnerabilities[0].VulnerabilityID == "CVE-1"
			},
			raw: `{"Results":[{"Target":"package-lock.json","Type":"npm","Vulnerabilities":[{"VulnerabilityID":"CVE-1","PkgName":"left-pad"}]}]}`,
		},
		{
			name: "grype",
			parse: func(raw string) bool {
				got, ok := ParseGrypeReport(raw)
				return ok && len(got.Matches) == 1 && got.Matches[0].Vulnerability.ID == "CVE-2"
			},
			raw: `{"matches":[{"vulnerability":{"id":"CVE-2","severity":"High"},"artifact":{"name":"left-pad","version":"1.0.0","type":"npm"}}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.parse(tt.raw) {
				t.Fatalf("expected %s parser to read report", tt.name)
			}
		})
	}
}

func TestParseScannerReportsRejectEmptyOrInvalid(t *testing.T) {
	if _, ok := ParseOSVReport(""); ok {
		t.Fatal("expected empty OSV report to be rejected")
	}
	if _, ok := ParseTrivyReport("not-json"); ok {
		t.Fatal("expected invalid Trivy report to be rejected")
	}
	if findings, ok := ParseGitleaksReport("[]"); !ok || len(findings) != 0 {
		t.Fatalf("expected empty gitleaks array to parse, got ok=%v findings=%#v", ok, findings)
	}
}
