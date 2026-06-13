package brew

import "testing"

func TestParseOutdatedReport(t *testing.T) {
	report, err := ParseOutdatedReport(`noise
{"formulae":[{"name":"git","installed_versions":["2.0"],"current_version":"2.1"}],"casks":[{"name":"app","installed_versions":"1.0","current_version":"1.1"}]}
warning`)
	if err != nil {
		t.Fatalf("ParseOutdatedReport() error = %v", err)
	}
	if len(report.Formulae) != 1 || report.Formulae[0].Name != "git" {
		t.Fatalf("unexpected formulae: %#v", report.Formulae)
	}
	if got := report.Formulae[0].InstalledVersions; len(got) != 1 || got[0] != "2.0" {
		t.Fatalf("unexpected formula installed versions: %#v", got)
	}
	if len(report.Casks) != 1 || report.Casks[0].Name != "app" {
		t.Fatalf("unexpected casks: %#v", report.Casks)
	}
	if got := report.Casks[0].InstalledVersions; len(got) != 1 || got[0] != "1.0" {
		t.Fatalf("unexpected cask installed versions: %#v", got)
	}
}

func TestParseOutdatedReportRejectsMissingJSON(t *testing.T) {
	if _, err := ParseOutdatedReport("Warning: not json"); err == nil {
		t.Fatal("expected missing JSON error")
	}
}
