package mise

import "testing"

func TestParseOutdatedReport(t *testing.T) {
	bump := "1.3.0"
	report, err := ParseOutdatedReport(`{
  "node": {"requested":"lts","current":"24.16.0","latest":"24.17.0"},
  "tool": {"requested":"1.2.0","current":"1.2.0","latest":"1.2.0","bump":"1.3.0"}
}`)
	if err != nil {
		t.Fatalf("ParseOutdatedReport() error = %v", err)
	}
	if report["node"].Requested != "lts" || report["node"].Latest != "24.17.0" {
		t.Fatalf("unexpected node item: %#v", report["node"])
	}
	if report["tool"].Bump == nil || *report["tool"].Bump != bump {
		t.Fatalf("unexpected bump item: %#v", report["tool"])
	}
}

func TestParseOutdatedReportRejectsInvalidJSON(t *testing.T) {
	if _, err := ParseOutdatedReport("not-json"); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}
