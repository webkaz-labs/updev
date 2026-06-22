package inventoryannotate

import (
	"strings"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

const profileMismatchDetailPrefix = "profile-mismatch:"

func AnnotateProfileScopedExtras(report *plan.Report, root string) {
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
		if !ok {
			continue
		}
		if item.Category == "" {
			item.Category = source.Category
		}
		detail := ProfileMismatchDetail(source.Category)
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
	items, err := brew.DesiredFromPath(updevpath.RootBrewfileTemplate(root))
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

func ProfileMismatchDetail(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		category = "source"
	}
	return profileMismatchDetailPrefix + " entry is defined in source deployment scope " + category + " but is not active in the current rendered Brewfile"
}

func ItemHasProfileMismatch(item plan.Item) bool {
	return strings.Contains(item.Detail, profileMismatchDetailPrefix)
}

func ItemStatusLabel(item plan.Item) string {
	if ItemHasProfileMismatch(item) {
		return "profile-mismatch"
	}
	return string(item.Status)
}
