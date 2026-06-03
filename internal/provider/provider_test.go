package provider

import (
	"context"
	"testing"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

type fakeProvider struct {
	name      string
	supported bool
	desired   []plan.Item
	live      []plan.Item
}

func (p fakeProvider) Name() string { return p.name }
func (p fakeProvider) Supported(context.Context) bool {
	return p.supported
}
func (p fakeProvider) Desired(context.Context) ([]plan.Item, error) { return p.desired, nil }
func (p fakeProvider) Live(context.Context) ([]plan.Item, error)    { return p.live, nil }

func TestCompareReportsMissingAndExtraItems(t *testing.T) {
	report := Compare(context.Background(), []Provider{fakeProvider{
		name:      "test",
		supported: true,
		desired: []plan.Item{
			{Kind: "tool", Name: "managed"},
			{Kind: "tool", Name: "missing"},
		},
		live: []plan.Item{
			{Kind: "tool", Name: "managed"},
			{Kind: "tool", Name: "extra"},
		},
	}})
	if report.Status != plan.StatusDrift {
		t.Fatalf("expected drift, got %s", report.Status)
	}
	if len(report.Providers) != 1 || report.Providers[0].Missing != 1 || report.Providers[0].Extra != 1 {
		t.Fatalf("unexpected provider summary: %#v", report.Providers)
	}
}

func TestComparePreservesProviderOrderWhenProvidersRunInParallel(t *testing.T) {
	report := Compare(context.Background(), []Provider{
		delayedProvider{
			fakeProvider: fakeProvider{
				name:      "slow",
				supported: true,
				desired:   []plan.Item{{Kind: "tool", Name: "slow-tool"}},
			},
			delay: 5 * time.Millisecond,
		},
		fakeProvider{
			name:      "fast",
			supported: true,
			desired:   []plan.Item{{Kind: "tool", Name: "fast-tool"}},
		},
	})
	if len(report.Providers) != 2 || report.Providers[0].Name != "slow" || report.Providers[1].Name != "fast" {
		t.Fatalf("expected provider order to match input order, got %#v", report.Providers)
	}
	if len(report.Items) != 2 || report.Items[0].Provider != "slow" || report.Items[1].Provider != "fast" {
		t.Fatalf("expected item order to match provider order, got %#v", report.Items)
	}
}

type delayedProvider struct {
	fakeProvider
	delay time.Duration
}

func (p delayedProvider) Desired(ctx context.Context) ([]plan.Item, error) {
	time.Sleep(p.delay)
	return p.fakeProvider.Desired(ctx)
}
