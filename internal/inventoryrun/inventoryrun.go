package inventoryrun

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/inventoryannotate"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/provider"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

const CacheVersion = 5

type Options struct {
	IncludeVSCode         bool
	UseHomeBrewfile       bool
	UseNativeMiseDesired  bool
	UseMisePackageDesired bool
	StateDir              *string
}

type CacheEntry struct {
	Version               int         `json:"version"`
	Root                  string      `json:"root"`
	IncludeVSCode         bool        `json:"include_vscode,omitempty"`
	UseMisePackageDesired bool        `json:"use_mise_package_desired,omitempty"`
	CreatedAt             time.Time   `json:"created_at"`
	Report                plan.Report `json:"report"`
}

type Result struct {
	Report    plan.Report `json:"report"`
	Cached    bool        `json:"cached"`
	CreatedAt time.Time   `json:"created_at,omitempty"`
}

func Collect(ctx context.Context, root string, commandRunner runner.Runner, opts Options) plan.Report {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var desiredResolver *brew.BootstrapDesiredResolver
	if opts.UseMisePackageDesired {
		desiredResolver = brew.NewBootstrapDesiredResolver(root, commandRunner)
	}
	report := provider.Compare(ctx, []provider.Provider{
		brew.Provider{Root: root, Runner: commandRunner, IncludeVSCode: opts.IncludeVSCode, UseHomeDesired: opts.UseHomeBrewfile, DesiredResolver: desiredResolver},
		mise.Provider{Root: root, Runner: commandRunner, UseNativeDesired: opts.UseNativeMiseDesired},
	})
	report.Root = root
	inventoryannotate.AnnotateProfileScopedExtras(&report, root)
	inventoryannotate.AnnotateMiseManifestIssues(&report, root)
	SortReport(&report)
	return report
}

func CollectCached(ctx context.Context, root string, refresh bool, maxAge time.Duration, commandRunner runner.Runner, opts Options) Result {
	if !refresh {
		if entry, ok := LoadCache(root, maxAge, opts); ok {
			inventoryannotate.AnnotateMiseManifestIssues(&entry.Report, root)
			SortReport(&entry.Report)
			return Result{Report: entry.Report, Cached: true, CreatedAt: entry.CreatedAt}
		}
	}
	report := Collect(ctx, root, commandRunner, opts)
	entry := CacheEntry{Version: CacheVersion, Root: root, IncludeVSCode: opts.IncludeVSCode, UseMisePackageDesired: opts.UseMisePackageDesired, CreatedAt: time.Now(), Report: report}
	SaveCache(entry, opts.StateDir)
	return Result{Report: report, Cached: false, CreatedAt: entry.CreatedAt}
}

func LoadCache(root string, maxAge time.Duration, opts Options) (CacheEntry, bool) {
	path := CachePath(root, opts.StateDir)
	if path == "" {
		return CacheEntry{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CacheEntry{}, false
	}
	var entry CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return CacheEntry{}, false
	}
	if entry.Version != CacheVersion || entry.Root != root || entry.IncludeVSCode != opts.IncludeVSCode || entry.UseMisePackageDesired != opts.UseMisePackageDesired {
		return CacheEntry{}, false
	}
	if maxAge > 0 && time.Since(entry.CreatedAt) > maxAge {
		return CacheEntry{}, false
	}
	return entry, true
}

func SaveCache(entry CacheEntry, stateDir *string) {
	path := CachePath(entry.Root, stateDir)
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

func CachePath(root string, stateDir *string) string {
	return updevpath.InventoryCacheFile(root, stateDir)
}

func SortReport(report *plan.Report) {
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
