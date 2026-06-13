package vscode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securityreason"
)

func TestPosturesFromItemsReportsMarketplaceRisks(t *testing.T) {
	requested := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request MarketplaceRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Filters) != 1 || len(request.Filters[0].Criteria) != 1 {
			t.Fatalf("unexpected marketplace request: %#v", request)
		}
		extension := request.Filters[0].Criteria[0].Value
		requested = append(requested, extension)
		switch extension {
		case "github.copilot":
			_, _ = w.Write([]byte(`{
  "results": [{
    "extensions": [{
      "publisher": {"publisherName": "github", "isDomainVerified": true},
      "extensionName": "copilot",
      "displayName": "GitHub Copilot",
      "flags": "validated, public",
      "lastUpdated": "2026-05-20T00:00:00Z",
      "publishedDate": "2021-06-29T00:00:00Z",
      "versions": [{"version": "1.388.0", "properties": [
        {"key": "Microsoft.VisualStudio.Code.ExecutesCode", "value": "true"},
        {"key": "Microsoft.VisualStudio.Services.Links.Source", "value": "https://github.com/github/copilot.vscode"}
      ]}],
      "statistics": [{"statisticName": "install", "value": 1000}, {"statisticName": "averagerating", "value": 4.2}]
    }]
  }]
}`))
		case "unknown.extension":
			_, _ = w.Write([]byte(`{"results":[{"extensions":[]}]}`))
		default:
			t.Fatalf("unexpected extension query: %s", extension)
		}
	}))
	defer server.Close()

	items := []plan.Item{
		{Provider: "brew", Kind: "vscode", Name: "github.copilot"},
		{Provider: "brew", Kind: "vscode", Name: "unknown.extension"},
		{Provider: "brew", Kind: "cask", Name: "visual-studio-code"},
	}
	postures, err := PosturesFromItems(context.Background(), server.Client(), server.URL, items, Thresholds{
		MinInstallCount:  1000,
		MinAverageRating: 2.0,
		MinExtensionAge:  14 * 24 * time.Hour,
	})
	if err == nil {
		t.Fatal("expected missing extension warning error")
	}
	if len(postures) != 2 {
		t.Fatalf("expected two VS Code posture entries, got %#v", postures)
	}
	byName := map[string]Posture{}
	for _, posture := range postures {
		byName[posture.Name] = posture
	}
	if byName["github.copilot"].Decision != "allow" || byName["github.copilot"].Version != "1.388.0" {
		t.Fatalf("expected allow posture for verified extension, got %#v", byName["github.copilot"])
	}
	if byName["github.copilot"].ReasonCode != securityreason.VSCodeMarketplaceAllowed {
		t.Fatalf("expected structured allow reason, got %#v", byName["github.copilot"])
	}
	if !byName["github.copilot"].ExecutesCode || byName["github.copilot"].RepositoryURL == "" {
		t.Fatalf("expected VS Code source metadata, got %#v", byName["github.copilot"])
	}
	if byName["unknown.extension"].Decision != "review" || !strings.Contains(byName["unknown.extension"].Reason, "metadata unavailable") {
		t.Fatalf("expected review posture for missing extension, got %#v", byName["unknown.extension"])
	}
	if byName["unknown.extension"].ReasonCode != securityreason.VSCodeMarketplaceUnavailable {
		t.Fatalf("expected structured unavailable reason code, got %#v", byName["unknown.extension"])
	}
	if len(requested) != 2 {
		t.Fatalf("expected two marketplace requests, got %#v", requested)
	}
}

func TestPostureFromMetadataReviewsLowTrustSignals(t *testing.T) {
	posture := PostureFromMetadata("publisher.extension", Extension{
		Publisher:     Publisher{PublisherName: "publisher", IsDomainVerified: false},
		ExtensionName: "extension",
		Flags:         "validated, public",
		Versions:      []Version{{Version: "1.0.0", Properties: []Property{{Key: "Microsoft.VisualStudio.Services.Links.Source", Value: "https://github.com/publisher/extension"}}}},
	}, Thresholds{MinInstallCount: 1000, MinAverageRating: 2.0})
	if posture.Decision != "review" || !strings.Contains(posture.Reason, "publisher domain") {
		t.Fatalf("expected publisher verification review, got %#v", posture)
	}
	if posture.ReasonCode != securityreason.VSCodePublisherUnverified || posture.ReasonArgs["extension"] != "publisher.extension" {
		t.Fatalf("expected structured publisher reason code, got %#v", posture)
	}
}
