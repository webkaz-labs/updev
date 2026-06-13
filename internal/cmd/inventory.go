package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/provider"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/updevpath"
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
	inventoryannotate.AnnotateProfileScopedExtras(&report, root)
	inventoryannotate.AnnotateMiseManifestIssues(&report, root)
	sortReport(&report)
	return report
}

func shouldUseHomeBrewfile(root string) bool {
	mode := "auto"
	if configured := loadUpdevConfig().Brewfile.Desired; configured != nil {
		mode = strings.ToLower(strings.TrimSpace(*configured))
	}
	switch mode {
	case "home":
		return true
	case "root", "template", "disabled":
		return false
	default:
		return filepath.Clean(root) == filepath.Clean(defaultRoot())
	}
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
			inventoryannotate.AnnotateMiseManifestIssues(&entry.Report, root)
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
	return updevpath.InventoryCacheFile(root, loadUpdevConfig().Inventory.StateDir)
}

func resolveUpdevConfigPath(root string, path string) string {
	return updevpath.Resolve(root, path)
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
