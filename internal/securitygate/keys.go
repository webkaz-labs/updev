package securitygate

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

type ItemIdentity struct {
	Kind string
	Name string
}

func BrewCandidateCacheKey(root string, findings []Finding, minReleaseAge time.Duration) string {
	parts := []string{"brew", root, "min-release-age-days=" + releaseAgeDays(minReleaseAge)}
	for _, finding := range findings {
		parts = append(parts, strings.Join([]string{
			finding.Kind,
			finding.Name,
			strings.Join(finding.InstalledVersions, ","),
			finding.CurrentVersion,
			finding.Source,
			finding.Tap,
			finding.URL,
		}, "\x1f"))
	}
	sort.Strings(parts[3:])
	return CacheKey(parts...)
}

func MiseCandidateCacheKey(root string, findings []Finding, minReleaseAge time.Duration) string {
	parts := []string{"mise", root, "mise-provider-metadata-v1", "min-release-age-days=" + releaseAgeDays(minReleaseAge)}
	for _, finding := range findings {
		parts = append(parts, strings.Join([]string{
			finding.Kind,
			finding.Name,
			strings.Join(finding.InstalledVersions, ","),
			finding.CurrentVersion,
			finding.Version,
			finding.Source,
			finding.RepositoryURL,
		}, "\x1f"))
	}
	sort.Strings(parts[4:])
	return CacheKey(parts...)
}

func VSCodeCandidateCacheKey(root string, items []ItemIdentity, installed map[string]string) string {
	parts := []string{"vscode", root}
	for _, item := range items {
		parts = append(parts, strings.Join([]string{item.Kind, item.Name, installed[strings.ToLower(item.Name)]}, "\x1f"))
	}
	sort.Strings(parts[2:])
	return CacheKey(parts...)
}

func ItemIdentitiesFromPlanItems(items []plan.Item) []ItemIdentity {
	out := make([]ItemIdentity, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		key := strings.ToLower(item.Kind + "\x00" + item.Name)
		if item.Name == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ItemIdentity{Kind: item.Kind, Name: item.Name})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func releaseAgeDays(value time.Duration) string {
	return strconv.Itoa(int(value.Hours() / 24))
}
