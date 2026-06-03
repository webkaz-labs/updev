package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/provider"
	"github.com/webkaz-labs/updev/internal/runner"
)

const inventoryCacheVersion = 4

type inventoryCacheEntry struct {
	Version       int         `json:"version"`
	Root          string      `json:"root"`
	IncludeVSCode bool        `json:"include_vscode,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	Report        plan.Report `json:"report"`
}

type inventoryResult struct {
	Report    plan.Report `json:"report"`
	Cached    bool        `json:"cached"`
	CreatedAt time.Time   `json:"created_at,omitempty"`
}

func collectInventory(ctx context.Context, root string, local runner.Local) plan.Report {
	return collectInventoryWithOptions(ctx, root, local, inventoryOptions{IncludeVSCode: includeVSCodeExtensionsByDefault()})
}

type inventoryOptions struct {
	IncludeVSCode bool
}

func collectInventoryWithOptions(ctx context.Context, root string, local runner.Local, opts inventoryOptions) plan.Report {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	report := provider.Compare(ctx, []provider.Provider{
		brew.Provider{Root: root, Runner: local, IncludeVSCode: opts.IncludeVSCode, UseHomeDesired: shouldUseHomeBrewfile(root)},
		mise.Provider{Root: root, Runner: local, UseNativeDesired: shouldUseNativeMiseDesired(root)},
	})
	report.Root = root
	annotateProfileScopedExtras(&report, root)
	annotateMiseManifestIssues(&report, root)
	sortReport(&report)
	return report
}

func shouldUseHomeBrewfile(root string) bool {
	return filepath.Clean(root) == filepath.Clean(defaultRoot())
}

func shouldUseNativeMiseDesired(root string) bool {
	cleanedRoot := filepath.Clean(root)
	cleanedDefault := filepath.Clean(defaultRoot())
	return cleanedRoot == cleanedDefault || strings.HasPrefix(cleanedRoot, cleanedDefault+string(os.PathSeparator))
}

func collectInventoryCached(ctx context.Context, root string, refresh bool, maxAge time.Duration) inventoryResult {
	return collectInventoryCachedWithOptions(ctx, root, refresh, maxAge, inventoryOptions{IncludeVSCode: includeVSCodeExtensionsByDefault()})
}

func collectInventoryCachedWithOptions(ctx context.Context, root string, refresh bool, maxAge time.Duration, opts inventoryOptions) inventoryResult {
	if !refresh {
		if entry, ok := loadInventoryCache(root, maxAge, opts); ok {
			annotateMiseManifestIssues(&entry.Report, root)
			sortReport(&entry.Report)
			return inventoryResult{Report: entry.Report, Cached: true, CreatedAt: entry.CreatedAt}
		}
	}
	report := collectInventoryWithOptions(ctx, root, runner.Local{}, opts)
	entry := inventoryCacheEntry{Version: inventoryCacheVersion, Root: root, IncludeVSCode: opts.IncludeVSCode, CreatedAt: time.Now(), Report: report}
	saveInventoryCache(entry)
	return inventoryResult{Report: report, Cached: false, CreatedAt: entry.CreatedAt}
}

func loadInventoryCache(root string, maxAge time.Duration, opts inventoryOptions) (inventoryCacheEntry, bool) {
	path := inventoryCachePath(root)
	if path == "" {
		return inventoryCacheEntry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return inventoryCacheEntry{}, false
	}
	var entry inventoryCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return inventoryCacheEntry{}, false
	}
	if entry.Version != inventoryCacheVersion || entry.Root != root || entry.IncludeVSCode != opts.IncludeVSCode {
		return inventoryCacheEntry{}, false
	}
	if maxAge > 0 && time.Since(entry.CreatedAt) > maxAge {
		return inventoryCacheEntry{}, false
	}
	return entry, true
}

func saveInventoryCache(entry inventoryCacheEntry) {
	path := inventoryCachePath(entry.Root)
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func inventoryCachePath(root string) string {
	if stateDir := loadUpdevConfig().Inventory.StateDir; stateDir != nil {
		if path := resolveUpdevConfigPath(root, *stateDir); path != "" {
			return filepath.Join(path, "inventory-v1.json")
		}
	}
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "updev", "inventory-v1.json")
}

func resolveUpdevConfigPath(root string, path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func annotateMiseManifestIssues(report *plan.Report, root string) {
	items := report.Items[:0]
	for _, item := range report.Items {
		if item.Provider == "mise" && item.Kind == "manifest" {
			continue
		}
		items = append(items, item)
	}
	report.Items = items
	issues, err := mise.ManifestIssues(root)
	if err != nil || len(issues) == 0 {
		return
	}
	for _, issue := range issues {
		report.Items = append(report.Items, plan.Item{
			Provider: "mise",
			Kind:     "manifest",
			Name:     issue.Tool,
			Category: issue.Backend,
			Version:  issue.Version,
			Desired:  true,
			Live:     true,
			Status:   plan.StatusBlocked,
			Detail:   miseManifestIssueDetail(issue),
		})
	}
	if report.Status != plan.StatusError {
		report.Status = plan.StatusDrift
	}
}

func miseManifestIssueDetail(issue mise.ManifestIssue) string {
	return issue.Path + ":" + strconv.Itoa(issue.Line) + ": " + issue.Reason
}

func sortReport(report *plan.Report) {
	sort.SliceStable(report.Providers, func(i, j int) bool {
		return report.Providers[i].Name < report.Providers[j].Name
	})
	sort.SliceStable(report.Items, func(i, j int) bool {
		left := report.Items[i]
		right := report.Items[j]
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		return left.Name < right.Name
	})
}
