package plan

import "testing"

func TestProviderStatus(t *testing.T) {
	tests := []struct {
		name     string
		provider ProviderSummary
		want     Status
	}{
		{name: "ok", provider: ProviderSummary{}, want: StatusOK},
		{name: "unavailable", provider: ProviderSummary{Unavailable: true}, want: StatusUnavailable},
		{name: "error", provider: ProviderSummary{Error: "failed"}, want: StatusError},
		{name: "drift", provider: ProviderSummary{Missing: 1}, want: StatusDrift},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProviderStatus(tt.provider); got != tt.want {
				t.Fatalf("ProviderStatus = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStatusMatches(t *testing.T) {
	if !StatusMatches(StatusHeld, "attention") {
		t.Fatalf("expected held to match attention")
	}
	if !StatusMatches(StatusMissing, "changed") {
		t.Fatalf("expected missing to match changed")
	}
	if StatusMatches(StatusOK, "attention") {
		t.Fatalf("did not expect ok to match attention")
	}
	if !StatusMatches(StatusOK, "OK") {
		t.Fatalf("expected status match to be case-insensitive")
	}
}

func TestItemMatchesQuery(t *testing.T) {
	item := Item{Name: "node", Kind: "tool", Category: "runtime", Version: "24.16.0", Detail: "JavaScript runtime"}
	for _, query := range []string{"", "NODE", "runtime", "24.16", "javascript"} {
		if !ItemMatchesQuery(item, query) {
			t.Fatalf("expected item to match query %q", query)
		}
	}
	if ItemMatchesQuery(item, "python") {
		t.Fatalf("did not expect item to match unrelated query")
	}
}
