package cmd

import (
	"path/filepath"
	"strings"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/plan"
)

const profileMismatchDetailPrefix = "profile-mismatch:"

func annotateProfileScopedExtras(report *plan.Report, root string) {
	if report == nil || root == "" {
		return
	}
	index := brewSourceDesiredIndex(root)
	if len(index) == 0 {
		return
	}
	for i := range report.Items {
		item := &report.Items[i]
		if !profileScopedExtraCandidate(*item) {
			continue
		}
		source, ok := index[profileScopeKey(*item)]
		if !ok || !strings.EqualFold(source.Category, "personal") {
			continue
		}
		if item.Category == "" {
			item.Category = source.Category
		}
		detail := profileMismatchDetail(source.Category)
		if strings.TrimSpace(item.Detail) == "" {
			item.Detail = detail
		} else if !strings.Contains(item.Detail, profileMismatchDetailPrefix) {
			item.Detail = detail + "; " + item.Detail
		}
	}
}

func profileScopedExtraCandidate(item plan.Item) bool {
	return strings.EqualFold(item.Provider, "brew") && item.Status == plan.StatusExtra && item.Kind != "" && item.Name != ""
}

func brewSourceDesiredIndex(root string) map[string]plan.Item {
	items, err := brew.DesiredFromPath(filepath.Join(root, "Brewfile.tmpl"))
	if err != nil {
		return nil
	}
	index := map[string]plan.Item{}
	for _, item := range items {
		index[profileScopeKey(item)] = item
	}
	return index
}

func profileScopeKey(item plan.Item) string {
	return strings.ToLower(strings.TrimSpace(item.Kind)) + "\x00" + strings.ToLower(strings.TrimSpace(item.Name))
}

func profileMismatchDetail(category string) string {
	return profileMismatchDetailPrefix + " entry is defined in " + category + " scope but is not active in the current rendered Brewfile"
}

func itemHasProfileMismatch(item plan.Item) bool {
	return strings.Contains(item.Detail, profileMismatchDetailPrefix)
}

func inventoryItemStatusLabel(item plan.Item) string {
	if itemHasProfileMismatch(item) {
		return "profile-mismatch"
	}
	return string(item.Status)
}
