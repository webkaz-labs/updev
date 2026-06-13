package inventoryannotate

import (
	"strconv"

	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/plan"
)

func AnnotateMiseManifestIssues(report *plan.Report, root string) {
	if report == nil || root == "" {
		return
	}
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
