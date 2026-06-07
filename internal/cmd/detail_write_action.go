package cmd

import (
	"fmt"
	"strings"
	"time"
)

type detailWriteActionSpec struct {
	Title          string
	Prompt         string
	Description    string
	NeedsReason    bool
	NeedsExpiry    bool
	DefaultReason  string
	DefaultExpires string
}

func routedDetailWriteActionSpec(value string) (detailWriteActionSpec, bool) {
	if action, target, ok := parseManualPlanDetailAction(value); ok {
		if action == "edit" || !manualPlanDetailActionRequiresConfirmation(action) {
			return detailWriteActionSpec{}, false
		}
		return detailWriteActionSpec{
			Title:       tr("manual inventory action", "manual inventory 操作"),
			Prompt:      fmt.Sprintf(tr("Apply %s to %s?", "%s を %s に適用しますか?"), action, target),
			Description: tr("Write the selected manual inventory override.", "選択した manual inventory override を書き込みます。"),
		}, true
	}
	if action, current, recommended, ok := parseBackendDetailAction(value); ok {
		if !backendDetailActionRequiresConfirmation(action) {
			return detailWriteActionSpec{}, false
		}
		switch action {
		case "remove-brew":
			return detailWriteActionSpec{
				Title:       tr("backend convergence action", "backend 整理操作"),
				Prompt:      fmt.Sprintf(tr("Remove Brewfile ownership %s? mise entry %s already exists.", "Brewfile ownership %s を削除しますか? mise entry %s は既に存在します。"), current, recommended),
				Description: tr("Remove desired-state ownership from Brewfile. This does not uninstall the Homebrew package.", "Brewfile の desired-state ownership を削除します。Homebrew package の uninstall は行いません。"),
			}, true
		case "rewrite-mise":
			return detailWriteActionSpec{
				Title:       tr("backend convergence action", "backend 整理操作"),
				Prompt:      fmt.Sprintf(tr("Rewrite mise backend %s -> %s?", "mise backend を %s -> %s に書き換えますか?"), current, recommended),
				Description: tr("Rename the mise tool key and preserve its current spec.", "現在の spec を維持したまま mise tool key を rename します。"),
			}, true
		case "remove-mise":
			return detailWriteActionSpec{
				Title:       tr("backend convergence action", "backend 整理操作"),
				Prompt:      fmt.Sprintf(tr("Remove old mise backend %s? Preferred entry %s already covers it.", "古い mise backend %s を削除しますか? 優先 entry %s が既にカバーしています。"), current, recommended),
				Description: tr("Remove the current mise tool key because the preferred entry already covers it.", "優先 entry がカバー済みのため、現在の mise tool key を削除します。"),
			}, true
		}
	}
	if action, provider, kind, name, ok := parseSecurityDetailAction(value); ok {
		if !securityDetailActionRequiresConfirmation(action) {
			return detailWriteActionSpec{}, false
		}
		decision, reason, expires, _ := defaultSecurityDetailActionInputs(action)
		spec := detailWriteActionSpec{
			Title:          tr("security policy action", "security policy 操作"),
			Prompt:         fmt.Sprintf(tr("Mark %s/%s %s as %s?", "%s/%s %s を %s にしますか?"), provider, kind, name, decision),
			Description:    tr("Write a local security policy rule.", "local security policy rule を書き込みます。"),
			DefaultReason:  reason,
			DefaultExpires: expires,
		}
		if expires != "" {
			spec.Description = fmt.Sprintf(tr("Write a local security policy rule that expires on %s.", "%s に期限切れになる local security policy rule を書き込みます。"), expires)
		}
		if securityDetailActionRequiresCustomInput(action) {
			spec.Prompt = fmt.Sprintf(tr("Allow %s/%s %s with a custom reason and expiry?", "%s/%s %s を理由/期限付きで許可しますか?"), provider, kind, name)
			spec.Description = tr("Enter a review reason and YYYY-MM-DD expiry before writing the local allow rule.", "local allow rule を書く前に review reason と YYYY-MM-DD expiry を入力します。")
			spec.NeedsReason = true
			spec.NeedsExpiry = true
			spec.DefaultReason = "accepted from updev detail browser after local review"
			spec.DefaultExpires = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
		}
		return spec, true
	}
	return detailWriteActionSpec{}, false
}

func applyRoutedDetailWriteAction(root string, report *updateReport, value string, reason string, expires string) bool {
	if action, target, ok := parseManualPlanDetailAction(value); ok {
		return applyConfirmedManualPlanDetailAction(root, action, target)
	}
	if action, current, recommended, ok := parseBackendDetailAction(value); ok {
		return applyConfirmedBackendDetailAction(root, action, current, recommended)
	}
	if action, provider, kind, name, ok := parseSecurityDetailAction(value); ok {
		if !securityDetailActionRequiresCustomInput(action) {
			_, reason, expires, _ = defaultSecurityDetailActionInputs(action)
		}
		return applyConfirmedSecurityDetailAction(report, action, provider, kind, name, strings.TrimSpace(reason), strings.TrimSpace(expires))
	}
	return false
}
