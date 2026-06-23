package updatereason

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	StrictGateReview        = "strict_gate_review"
	StrictGateFailed        = "strict_gate_failed"
	StrictMiseNoSafe        = "strict_mise_no_safe"
	StrictBrewNoSafe        = "strict_brew_no_safe"
	StrictBrewRefreshOnly   = "strict_brew_refresh_only"
	StrictBrewRefreshDone   = "strict_brew_refresh_done"
	StrictBrewRefreshFailed = "strict_brew_refresh_failed"
	StrictBrewNoCandidates  = "strict_brew_no_candidates"
	StrictBrewHeld          = "strict_brew_held"
	StrictMisePartial       = "strict_mise_partial"
	StrictBrewPartial       = "strict_brew_partial"
	StrictDryRunPartial     = "strict_dry_run_partial"
	StrictAppliedPartial    = "strict_applied_partial"

	MiseBumpManual                   = "mise_bump_manual"
	MiseBumpSafeManual               = "mise_bump_safe_manual"
	MiseBumpReview                   = "mise_bump_review"
	MiseBumpReviewNoSafeAuto         = "mise_bump_review_no_safe_auto"
	MiseBumpAutoWouldApply           = "mise_bump_auto_would_apply"
	MiseBumpAutoWouldApplyWithReview = "mise_bump_auto_would_apply_with_review"
	MiseBumpCandidateChangedApply    = "mise_bump_candidate_changed_apply"
	MiseBumpCandidateChangedPreview  = "mise_bump_candidate_changed_preview"
	MiseBumpDependencyBlockedOnly    = "mise_bump_dependency_blocked_only"
	MiseBumpPreflightFailed          = "mise_bump_preflight_failed"
	MiseBumpFailed                   = "mise_bump_failed"
	MiseBumpAppliedWithReview        = "mise_bump_applied_with_review"

	SecurityPolicyScopedRerun = "security_policy_scoped_rerun"
)

type Reason struct {
	Code string
	Text string
	Args map[string]string
}

func New(code string, text string, args map[string]string) Reason {
	return Reason{Code: code, Text: text, Args: cleanArgs(args)}
}

func StrictGateReviewReason() Reason {
	return New(StrictGateReview, "security=strict held update because safety gate requires review", nil)
}

func StrictGateFailedReason(err string) Reason {
	summary := summarizeGateError(err)
	return New(StrictGateFailed, "security=strict held update because safety gate failed: "+summary, map[string]string{"error": err, "summary": summary})
}

func StrictMiseNoSafeReason() Reason {
	return New(StrictMiseNoSafe, "security=strict held mise update because no scoped safe candidates were found", nil)
}

func StrictBrewNoSafeReason() Reason {
	return New(StrictBrewNoSafe, "security=strict held brew update because no scoped safe candidates were found", nil)
}

func StrictBrewRefreshOnlyReason() Reason {
	return New(StrictBrewRefreshOnly, "strict safety refreshes Homebrew metadata only before rechecking package candidates", nil)
}

func StrictBrewRefreshDoneReason() Reason {
	return New(StrictBrewRefreshDone, "strict safety refreshed Homebrew metadata before rechecking package candidates", nil)
}

func StrictBrewRefreshFailedReason(err string) Reason {
	return New(StrictBrewRefreshFailed, "Homebrew safety gate failed after metadata refresh: "+err, map[string]string{"error": err})
}

func StrictBrewNoCandidatesReason() Reason {
	return New(StrictBrewNoCandidates, "strict safety refreshed Homebrew metadata; no package candidates found", nil)
}

func StrictBrewHeldReason(count int) Reason {
	return New(StrictBrewHeld, fmt.Sprintf("strict safety refreshed Homebrew metadata and held %d Homebrew candidates requiring review", count), map[string]string{"held": strconv.Itoa(count)})
}

func StrictMisePartialReason(safe int, unsafe int) Reason {
	return New(StrictMisePartial, fmt.Sprintf("strict safety will apply %d safe mise candidates and hold %d unsafe candidates", safe, unsafe), countArgs(safe, unsafe))
}

func StrictBrewPartialReason(safe int, unsafe int) Reason {
	return New(StrictBrewPartial, fmt.Sprintf("strict safety will apply %d safe Homebrew candidates and hold %d unsafe candidates; Homebrew cannot generally install an older intermediate release", safe, unsafe), countArgs(safe, unsafe))
}

func StrictDryRunPartialReason() Reason {
	return New(StrictDryRunPartial, "strict safety would apply safe candidates and hold unsafe candidates", nil)
}

func StrictAppliedPartialReason() Reason {
	return New(StrictAppliedPartial, "strict safety applied safe candidates and held unsafe candidates", nil)
}

func MiseBumpManualReason() Reason {
	return New(MiseBumpManual, "mise bump candidates available; mode=manual requires item review", nil)
}

func MiseBumpSafeManualReason(safe int) Reason {
	return New(MiseBumpSafeManual, fmt.Sprintf("mise bump candidates available; %d safe candidates can be applied after confirmation", safe), map[string]string{"safe": strconv.Itoa(safe)})
}

func MiseBumpReviewReason() Reason {
	return New(MiseBumpReview, "mise bump candidates require review", nil)
}

func MiseBumpReviewNoSafeAutoReason() Reason {
	return New(MiseBumpReviewNoSafeAuto, "mise bump candidates require review; no safe auto candidates", nil)
}

func MiseBumpAutoWouldApplyReason(safe int) Reason {
	return New(MiseBumpAutoWouldApply, fmt.Sprintf("mise bump auto would apply %d safe candidates", safe), map[string]string{"safe": strconv.Itoa(safe)})
}

func MiseBumpAutoWouldApplyWithReviewReason(safe int, review int) Reason {
	return New(MiseBumpAutoWouldApplyWithReview, fmt.Sprintf("mise bump auto would apply %d safe candidates; %d candidates require review", safe, review), map[string]string{"safe": strconv.Itoa(safe), "review": strconv.Itoa(review)})
}

func MiseBumpCandidateChangedApplyReason(detail string) Reason {
	return New(MiseBumpCandidateChangedApply, "mise bump candidate set changed before apply: "+detail, map[string]string{"detail": detail})
}

func MiseBumpCandidateChangedPreviewReason(detail string) Reason {
	return New(MiseBumpCandidateChangedPreview, "mise bump candidate set changed before preview: "+detail, map[string]string{"detail": detail})
}

func MiseBumpDependencyBlockedOnlyReason() Reason {
	return New(MiseBumpDependencyBlockedOnly, "mise bump auto found only dependency-blocked candidates", nil)
}

func MiseBumpPreflightFailedReason(detail string) Reason {
	return New(MiseBumpPreflightFailed, "mise bump dry-run preflight failed: "+detail, map[string]string{"detail": detail})
}

func MiseBumpFailedReason(detail string) Reason {
	return New(MiseBumpFailed, "mise bump failed: "+detail, map[string]string{"detail": detail})
}

func MiseBumpAppliedWithReviewReason(safe int, review int) Reason {
	return New(MiseBumpAppliedWithReview, fmt.Sprintf("mise bump applied %d safe candidates; %d candidates require review", safe, review), map[string]string{"safe": strconv.Itoa(safe), "review": strconv.Itoa(review)})
}

func SecurityPolicyScopedRerunReason(provider string, kind string, name string) Reason {
	text := fmt.Sprintf("security policy reran scoped %s update for %s/%s %s", provider, provider, kind, name)
	return New(SecurityPolicyScopedRerun, text, map[string]string{"provider": provider, "kind": kind, "name": name})
}

func Infer(text string) Reason {
	text = strings.TrimSpace(text)
	switch text {
	case "":
		return Reason{}
	case StrictGateReviewReason().Text:
		return StrictGateReviewReason()
	case StrictMiseNoSafeReason().Text:
		return StrictMiseNoSafeReason()
	case StrictBrewNoSafeReason().Text:
		return StrictBrewNoSafeReason()
	case StrictBrewRefreshOnlyReason().Text:
		return StrictBrewRefreshOnlyReason()
	case StrictBrewRefreshDoneReason().Text:
		return StrictBrewRefreshDoneReason()
	case StrictBrewRefreshFailedReason("").Text:
		return StrictBrewRefreshFailedReason("")
	case StrictBrewNoCandidatesReason().Text:
		return StrictBrewNoCandidatesReason()
	case StrictDryRunPartialReason().Text:
		return StrictDryRunPartialReason()
	case StrictAppliedPartialReason().Text:
		return StrictAppliedPartialReason()
	case MiseBumpManualReason().Text:
		return MiseBumpManualReason()
	case MiseBumpReviewReason().Text:
		return MiseBumpReviewReason()
	case MiseBumpReviewNoSafeAutoReason().Text:
		return MiseBumpReviewNoSafeAutoReason()
	case MiseBumpDependencyBlockedOnlyReason().Text:
		return MiseBumpDependencyBlockedOnlyReason()
	}
	if suffix, ok := strings.CutPrefix(text, "security=strict held update because safety gate failed: "); ok {
		return StrictGateFailedReason(suffix)
	}
	if suffix, ok := strings.CutPrefix(text, "Homebrew safety gate failed after metadata refresh: "); ok {
		return StrictBrewRefreshFailedReason(suffix)
	}
	if suffix, ok := strings.CutPrefix(text, "mise bump candidate set changed before apply: "); ok {
		return MiseBumpCandidateChangedApplyReason(suffix)
	}
	if suffix, ok := strings.CutPrefix(text, "mise bump candidate set changed before preview: "); ok {
		return MiseBumpCandidateChangedPreviewReason(suffix)
	}
	if suffix, ok := strings.CutPrefix(text, "mise bump dry-run preflight failed: "); ok {
		return MiseBumpPreflightFailedReason(suffix)
	}
	if suffix, ok := strings.CutPrefix(text, "mise bump failed: "); ok {
		return MiseBumpFailedReason(suffix)
	}
	if count, ok := parseOneCount(text, "strict safety refreshed Homebrew metadata and held ", " Homebrew candidates requiring review"); ok {
		return StrictBrewHeldReason(count)
	}
	if safe, unsafe, ok := parseTwoCounts(text, "strict safety will apply ", " safe mise candidates and hold ", " unsafe candidates"); ok {
		return StrictMisePartialReason(safe, unsafe)
	}
	if safe, unsafe, ok := parseTwoCounts(text, "strict safety will apply ", " safe Homebrew candidates and hold ", " unsafe candidates; Homebrew cannot generally install an older intermediate release"); ok {
		return StrictBrewPartialReason(safe, unsafe)
	}
	if count, ok := parseOneCount(text, "mise bump candidates available; ", " safe candidates can be applied after confirmation"); ok {
		return MiseBumpSafeManualReason(count)
	}
	if count, ok := parseOneCount(text, "mise bump auto would apply ", " safe candidates"); ok {
		return MiseBumpAutoWouldApplyReason(count)
	}
	if safe, review, ok := parseTwoCounts(text, "mise bump auto would apply ", " safe candidates; ", " candidates require review"); ok {
		return MiseBumpAutoWouldApplyWithReviewReason(safe, review)
	}
	if safe, review, ok := parseTwoCounts(text, "mise bump applied ", " safe candidates; ", " candidates require review"); ok {
		return MiseBumpAppliedWithReviewReason(safe, review)
	}
	if provider, kind, name, ok := parseSecurityPolicyScopedRerun(text); ok {
		return SecurityPolicyScopedRerunReason(provider, kind, name)
	}
	return Reason{Text: text}
}

func LocalizeJapanese(reason Reason) string {
	if reason.Code == "" {
		return reason.Text
	}
	switch reason.Code {
	case StrictGateReview:
		return "security=strict のため更新を保留しました: safety gate の確認が必要です"
	case StrictGateFailed:
		summary := arg(reason, "summary")
		if summary == "" {
			summary = summarizeGateError(arg(reason, "error"))
		}
		return "security=strict のため更新を保留しました: safety gate が失敗しました: " + summary
	case StrictBrewRefreshDone:
		return "strict safety のため Homebrew metadata を更新し、package 候補を再確認しました"
	case StrictBrewRefreshFailed:
		return "Homebrew metadata 更新後の safety gate が失敗しました: " + arg(reason, "error")
	case StrictBrewRefreshOnly:
		return "strict safety のため Homebrew metadata の更新だけを実行し、package 候補を再確認します"
	case StrictBrewNoCandidates:
		return "strict safety のため Homebrew metadata を更新しました。更新対象の package 候補はありません"
	case StrictBrewHeld:
		return "Homebrew metadata を更新し、確認が必要な Homebrew 候補 " + arg(reason, "held") + "件を保留しました"
	case StrictMiseNoSafe:
		return "security=strict のため mise 更新を保留しました: 適用できる scoped safe 候補がありません"
	case StrictBrewNoSafe:
		return "security=strict のため brew 更新を保留しました: 適用できる scoped safe 候補がありません"
	case StrictMisePartial:
		return fmt.Sprintf("strict safety は mise の safe 候補 %s件だけを適用し、unsafe 候補 %s件を保留します", arg(reason, "safe"), arg(reason, "unsafe"))
	case StrictBrewPartial:
		return fmt.Sprintf("strict safety は Homebrew の safe 候補 %s件だけを適用し、unsafe 候補 %s件を保留します。Homebrew は通常、古い中間 version を指定して install できません", arg(reason, "safe"), arg(reason, "unsafe"))
	case StrictDryRunPartial:
		return "strict safety は safe 候補だけを適用し unsafe 候補を hold します"
	case StrictAppliedPartial:
		return "strict safety は safe 候補を適用し unsafe 候補を hold しました"
	case MiseBumpManual:
		return "mise bump 候補があります。mode=manual のため item ごとの確認が必要です"
	case MiseBumpReview:
		return "mise bump 候補の確認が必要です"
	case MiseBumpReviewNoSafeAuto:
		return "mise bump 候補の確認が必要です。自動適用できる safe 候補はありません"
	case MiseBumpSafeManual:
		return "mise bump 候補があります。確認後に safe 候補 " + arg(reason, "safe") + "件を適用できます"
	case MiseBumpAutoWouldApply:
		return "mise bump auto は safe 候補 " + arg(reason, "safe") + "件を適用します"
	case MiseBumpAutoWouldApplyWithReview:
		return fmt.Sprintf("mise bump auto は safe 候補 %s件を適用し、%s件は確認待ちにします", arg(reason, "safe"), arg(reason, "review"))
	case MiseBumpCandidateChangedApply:
		return "mise bump の候補が適用直前に変わったため保留しました: " + arg(reason, "detail")
	case MiseBumpCandidateChangedPreview:
		return "mise bump の候補が preview 直前に変わりました: " + arg(reason, "detail")
	case MiseBumpDependencyBlockedOnly:
		return "mise bump auto で見つかった候補は dependency 不足で block されたものだけです"
	case MiseBumpPreflightFailed:
		return "mise bump の dry-run preflight が失敗しました: " + arg(reason, "detail")
	case MiseBumpFailed:
		return "mise bump が失敗しました: " + arg(reason, "detail")
	case MiseBumpAppliedWithReview:
		return fmt.Sprintf("mise bump は safe 候補 %s件を適用し、%s件は確認待ちです", arg(reason, "safe"), arg(reason, "review"))
	case SecurityPolicyScopedRerun:
		return fmt.Sprintf("security policy に従い、%s の scoped update を再実行しました: %s/%s %s", arg(reason, "provider"), arg(reason, "provider"), arg(reason, "kind"), arg(reason, "name"))
	default:
		return reason.Text
	}
}

func cleanArgs(args map[string]string) map[string]string {
	if len(args) == 0 {
		return nil
	}
	out := map[string]string{}
	for key, value := range args {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func countArgs(safe int, unsafe int) map[string]string {
	return map[string]string{"safe": strconv.Itoa(safe), "unsafe": strconv.Itoa(unsafe)}
}

func arg(reason Reason, key string) string {
	if reason.Args == nil {
		return ""
	}
	return reason.Args[key]
}

func summarizeGateError(err string) string {
	err = strings.TrimSpace(err)
	if err == "" {
		return "provider metadata check failed"
	}
	lower := strings.ToLower(err)
	switch {
	case strings.Contains(lower, "npm error a complete log of this run can be found in:"):
		return "npm metadata probe failed; debug logs are available in expanded evidence"
	case strings.Contains(lower, "crates.io"):
		return firstLine(err)
	case strings.Contains(lower, "pypi"):
		return firstLine(err)
	default:
		return firstLine(err)
	}
}

func firstLine(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if line, _, ok := strings.Cut(value, "\n"); ok {
		return strings.TrimSpace(line)
	}
	return value
}

func parseOneCount(text string, prefix string, suffix string) (int, bool) {
	value, ok := strings.CutPrefix(text, prefix)
	if !ok {
		return 0, false
	}
	value, ok = strings.CutSuffix(value, suffix)
	if !ok {
		return 0, false
	}
	count, err := strconv.Atoi(strings.TrimSpace(value))
	return count, err == nil
}

func parseTwoCounts(text string, prefix string, middle string, suffix string) (int, int, bool) {
	value, ok := strings.CutPrefix(text, prefix)
	if !ok {
		return 0, 0, false
	}
	left, right, ok := strings.Cut(value, middle)
	if !ok {
		return 0, 0, false
	}
	right, ok = strings.CutSuffix(right, suffix)
	if !ok {
		return 0, 0, false
	}
	first, err := strconv.Atoi(strings.TrimSpace(left))
	if err != nil {
		return 0, 0, false
	}
	second, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil {
		return 0, 0, false
	}
	return first, second, true
}

func parseSecurityPolicyScopedRerun(text string) (string, string, string, bool) {
	value, ok := strings.CutPrefix(text, "security policy reran scoped ")
	if !ok {
		return "", "", "", false
	}
	provider, rest, ok := strings.Cut(value, " update for ")
	if !ok {
		return "", "", "", false
	}
	identity := strings.Fields(rest)
	if len(identity) != 2 {
		return "", "", "", false
	}
	parts := strings.SplitN(identity[0], "/", 2)
	if len(parts) != 2 || parts[0] != provider {
		return "", "", "", false
	}
	return provider, parts[1], identity[1], true
}
