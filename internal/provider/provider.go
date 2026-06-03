package provider

import (
	"context"
	"sync"

	"github.com/webkaz-labs/updev/internal/plan"
)

type Provider interface {
	Name() string
	Supported(ctx context.Context) bool
	Desired(ctx context.Context) ([]plan.Item, error)
	Live(ctx context.Context) ([]plan.Item, error)
}

func Compare(ctx context.Context, providers []Provider) plan.Report {
	report := plan.Report{Status: plan.StatusOK}
	results := make([]providerCompareResult, len(providers))
	var wg sync.WaitGroup
	for index, current := range providers {
		wg.Add(1)
		go func(index int, provider Provider) {
			defer wg.Done()
			results[index] = compareProvider(ctx, provider)
		}(index, current)
	}
	wg.Wait()
	for _, result := range results {
		report.Providers = append(report.Providers, result.Summary)
		report.Items = append(report.Items, result.Items...)
		switch {
		case result.Summary.Error != "":
			report.Status = plan.StatusError
		case report.Status != plan.StatusError && (result.Summary.Missing > 0 || result.Summary.Extra > 0):
			report.Status = plan.StatusDrift
		}
	}
	return report
}

type providerCompareResult struct {
	Summary plan.ProviderSummary
	Items   []plan.Item
}

func compareProvider(ctx context.Context, provider Provider) providerCompareResult {
	summary := plan.ProviderSummary{Name: provider.Name()}
	if !provider.Supported(ctx) {
		summary.Unavailable = true
		return providerCompareResult{Summary: summary}
	}
	summary.Supported = true
	desired, err := provider.Desired(ctx)
	if err != nil {
		summary.Error = err.Error()
		return providerCompareResult{Summary: summary}
	}
	live, err := provider.Live(ctx)
	if err != nil {
		summary.Error = err.Error()
		return providerCompareResult{Summary: summary}
	}
	summary.Desired = len(desired)
	summary.Live = len(live)
	items, missing, extra := compareItems(provider.Name(), desired, live)
	summary.Missing = missing
	summary.Extra = extra
	return providerCompareResult{Summary: summary, Items: items}
}

func compareItems(providerName string, desired []plan.Item, live []plan.Item) ([]plan.Item, int, int) {
	liveByKey := map[string]plan.Item{}
	desiredByKey := map[string]plan.Item{}
	for _, item := range live {
		liveByKey[item.Kind+"\x00"+item.Name] = item
	}
	for _, item := range desired {
		desiredByKey[item.Kind+"\x00"+item.Name] = item
	}
	items := make([]plan.Item, 0, len(desired)+len(live))
	missing := 0
	for key, item := range desiredByKey {
		item.Provider = providerName
		item.Desired = true
		if _, ok := liveByKey[key]; ok {
			item.Live = true
			item.Status = plan.StatusOK
		} else {
			missing++
			item.Status = plan.StatusMissing
		}
		items = append(items, item)
	}
	extra := 0
	for key, item := range liveByKey {
		if _, ok := desiredByKey[key]; ok {
			continue
		}
		extra++
		item.Provider = providerName
		item.Live = true
		item.Status = plan.StatusExtra
		items = append(items, item)
	}
	return items, missing, extra
}
