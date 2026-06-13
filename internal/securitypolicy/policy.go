package securitypolicy

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/securitygate"
)

type Policy struct {
	Rules []Rule `json:"rules"`
}

type Rule struct {
	Provider string `json:"provider"`
	Kind     string `json:"kind,omitempty"`
	Name     string `json:"name"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Expires  string `json:"expires,omitempty"`
	Line     int    `json:"-"`
}

type RuleView struct {
	Index         int    `json:"index"`
	Line          int    `json:"line,omitempty"`
	Provider      string `json:"provider,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Name          string `json:"name"`
	Decision      string `json:"decision"`
	State         string `json:"state"`
	Reason        string `json:"reason,omitempty"`
	Expires       string `json:"expires,omitempty"`
	Active        bool   `json:"active"`
	Invalid       bool   `json:"invalid,omitempty"`
	Expired       bool   `json:"expired,omitempty"`
	Duplicate     bool   `json:"duplicate,omitempty"`
	Shadowed      bool   `json:"shadowed,omitempty"`
	ShadowedBy    int    `json:"shadowed_by,omitempty"`
	MissingReason bool   `json:"missing_reason,omitempty"`
	MissingExpiry bool   `json:"missing_expiry,omitempty"`
	Broad         bool   `json:"broad,omitempty"`
	Remediation   string `json:"remediation,omitempty"`
}

type RuleCounts struct {
	Active        int
	Expired       int
	Invalid       int
	Duplicate     int
	Shadowed      int
	MissingReason int
	MissingExpiry int
	Broad         int
}

type Summary struct {
	RuleCount       int `json:"rule_count"`
	FilteredRules   int `json:"filtered_rules,omitempty"`
	ActiveRules     int `json:"active_rules"`
	ExpiredRules    int `json:"expired_rules,omitempty"`
	InvalidRules    int `json:"invalid_rules,omitempty"`
	DuplicateRules  int `json:"duplicate_rules,omitempty"`
	ShadowedRules   int `json:"shadowed_rules,omitempty"`
	MissingReasons  int `json:"missing_reasons,omitempty"`
	MissingExpiries int `json:"missing_expiries,omitempty"`
	BroadRules      int `json:"broad_rules,omitempty"`
}

type indexedRule struct {
	index int
	rule  Rule
}

func Read(path string) (Policy, error) {
	if strings.TrimSpace(path) == "" {
		return Policy{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Policy{}, nil
		}
		return Policy{}, err
	}
	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return Policy{}, err
	}
	lines := RuleLineNumbers(data)
	for index := range policy.Rules {
		if index < len(lines) {
			policy.Rules[index].Line = lines[index]
		}
	}
	return policy, nil
}

func Write(path string, policy Policy) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func RuleLineNumbers(data []byte) []int {
	var raw struct {
		Rules json.RawMessage `json:"rules"`
	}
	if err := json.Unmarshal(data, &raw); err != nil || len(raw.Rules) == 0 {
		return nil
	}
	start := bytes.Index(data, raw.Rules)
	if start < 0 {
		return nil
	}
	baseLine := 1 + bytes.Count(data[:start], []byte("\n"))
	lines := []int{}
	inString := false
	escaped := false
	arrayDepth := 0
	objectDepth := 0
	for index, char := range raw.Rules {
		if inString {
			switch {
			case escaped:
				escaped = false
			case char == '\\':
				escaped = true
			case char == '"':
				inString = false
			}
			continue
		}
		switch char {
		case '"':
			inString = true
		case '[':
			arrayDepth++
		case ']':
			if arrayDepth > 0 {
				arrayDepth--
			}
		case '{':
			if arrayDepth == 1 && objectDepth == 0 {
				lines = append(lines, baseLine+bytes.Count(raw.Rules[:index], []byte("\n")))
			}
			objectDepth++
		case '}':
			if objectDepth > 0 {
				objectDepth--
			}
		}
	}
	return lines
}

func RuleViews(policy Policy) []RuleView {
	views := make([]RuleView, 0, len(policy.Rules))
	seenRules := map[string]bool{}
	activeRules := []indexedRule{}
	for index, rawRule := range policy.Rules {
		rule := NormalizeRule(rawRule)
		view := RuleView{
			Index:    index + 1,
			Line:     rawRule.Line,
			Provider: rule.Provider,
			Kind:     rule.Kind,
			Name:     rule.Name,
			Decision: rule.Decision,
			Reason:   rule.Reason,
			Expires:  rule.Expires,
		}
		expired, invalidExpires := RuleExpiryState(rule)
		view.Expired = expired
		view.Invalid = invalidExpires || !securitygate.ValidDecision(view.Decision) || rule.Name == ""
		key := RuleKey(rule)
		if !view.Invalid && !view.Expired {
			view.Duplicate = seenRules[key]
		}
		if !view.Invalid && !view.Expired && !view.Duplicate {
			view.ShadowedBy = ruleShadowedBy(rule, activeRules)
			view.Shadowed = view.ShadowedBy > 0
		}
		view.Active = !view.Invalid && !view.Expired && !view.Duplicate && !view.Shadowed
		view.MissingReason = RuleMissingReason(view)
		view.MissingExpiry = RuleMissingExpiry(view)
		view.Broad = RuleBroad(view)
		view.State = RuleState(view)
		if view.Active {
			activeRules = append(activeRules, indexedRule{index: view.Index, rule: rule})
			seenRules[key] = true
		}
		views = append(views, view)
	}
	return views
}

func ruleShadowedBy(rule Rule, previous []indexedRule) int {
	for _, earlier := range previous {
		if RuleCovers(earlier.rule, rule) {
			return earlier.index
		}
	}
	return 0
}

func RuleCovers(earlier Rule, later Rule) bool {
	return FieldCovers(earlier.Provider, later.Provider) &&
		FieldCovers(earlier.Kind, later.Kind) &&
		NameCovers(earlier.Name, later.Name)
}

func FieldCovers(earlier string, later string) bool {
	if earlier == "" {
		return true
	}
	return later != "" && strings.EqualFold(earlier, later)
}

func NameCovers(earlier string, later string) bool {
	if earlier == "*" {
		return true
	}
	return later != "*" && strings.EqualFold(earlier, later)
}

func RuleMissingReason(view RuleView) bool {
	return view.Active && view.Decision == "allow" && strings.TrimSpace(view.Reason) == ""
}

func RuleMissingExpiry(view RuleView) bool {
	return view.Active && DecisionNeedsExpiry(view.Decision) && strings.TrimSpace(view.Expires) == ""
}

func DecisionNeedsExpiry(decision string) bool {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "allow", "review", "hold":
		return true
	default:
		return false
	}
}

func RuleBroad(view RuleView) bool {
	return view.Active && (strings.TrimSpace(view.Provider) == "" || strings.TrimSpace(view.Name) == "*")
}

func RuleState(view RuleView) string {
	switch {
	case view.Invalid:
		return "invalid"
	case view.Expired:
		return "expired"
	case view.Duplicate:
		return "duplicate"
	case view.Shadowed:
		return "shadowed"
	default:
		return "active"
	}
}

func RuleNeedsCleanup(view RuleView) bool {
	return view.Invalid ||
		view.Expired ||
		view.Duplicate ||
		view.Shadowed ||
		view.MissingReason ||
		view.MissingExpiry ||
		view.Broad
}

func RuleCountsForViews(views []RuleView) RuleCounts {
	var summary RuleCounts
	for _, view := range views {
		switch {
		case view.Invalid:
			summary.Invalid++
		case view.Expired:
			summary.Expired++
		case view.Duplicate:
			summary.Duplicate++
		case view.Shadowed:
			summary.Shadowed++
		case view.Active:
			summary.Active++
		}
		if view.MissingReason {
			summary.MissingReason++
		}
		if view.MissingExpiry {
			summary.MissingExpiry++
		}
		if view.Broad {
			summary.Broad++
		}
	}
	return summary
}

func RuleCountsForPolicy(policy Policy) RuleCounts {
	return RuleCountsForViews(RuleViews(policy))
}

func SummaryFromViews(views []RuleView) *Summary {
	counts := RuleCountsForViews(views)
	return &Summary{
		RuleCount:       len(views),
		ActiveRules:     counts.Active,
		ExpiredRules:    counts.Expired,
		InvalidRules:    counts.Invalid,
		DuplicateRules:  counts.Duplicate,
		ShadowedRules:   counts.Shadowed,
		MissingReasons:  counts.MissingReason,
		MissingExpiries: counts.MissingExpiry,
		BroadRules:      counts.Broad,
	}
}

func NormalizeRule(rule Rule) Rule {
	rule.Provider = strings.TrimSpace(rule.Provider)
	rule.Kind = strings.TrimSpace(rule.Kind)
	rule.Name = strings.TrimSpace(rule.Name)
	rule.Decision = strings.ToLower(strings.TrimSpace(rule.Decision))
	if rule.Decision == "deny" {
		rule.Decision = "block"
	}
	rule.Reason = strings.TrimSpace(rule.Reason)
	rule.Expires = strings.TrimSpace(rule.Expires)
	return rule
}

func RuleKey(rule Rule) string {
	return strings.Join([]string{
		strings.ToLower(rule.Provider),
		strings.ToLower(rule.Kind),
		strings.ToLower(rule.Name),
	}, "\x00")
}

func RuleExpired(rule Rule) bool {
	expired, invalid := RuleExpiryState(rule)
	return expired || invalid
}

func RuleExpiryState(rule Rule) (bool, bool) {
	if strings.TrimSpace(rule.Expires) == "" {
		return false, false
	}
	expires, err := time.Parse("2006-01-02", strings.TrimSpace(rule.Expires))
	if err != nil {
		return false, true
	}
	return !time.Now().Before(expires.Add(24 * time.Hour)), false
}

func MatchingRule(policy Policy, provider string, kind string, name string) (Rule, bool) {
	for _, rawRule := range policy.Rules {
		rule := NormalizeRule(rawRule)
		expired, invalidExpires := RuleExpiryState(rule)
		if expired || invalidExpires {
			continue
		}
		if rule.Provider != "" && !strings.EqualFold(rule.Provider, provider) {
			continue
		}
		if rule.Kind != "" && !strings.EqualFold(rule.Kind, kind) {
			continue
		}
		if rule.Name != "*" && !strings.EqualFold(rule.Name, name) {
			continue
		}
		decision := strings.ToLower(strings.TrimSpace(rule.Decision))
		if !securitygate.ValidDecision(decision) {
			continue
		}
		rule.Decision = decision
		return rule, true
	}
	return Rule{}, false
}

func ProviderForEcosystem(ecosystem string) string {
	switch strings.ToLower(strings.TrimSpace(ecosystem)) {
	case "npm":
		return "npm"
	case "crates.io":
		return "cargo"
	case "pypi":
		return "pypi"
	default:
		return ecosystem
	}
}
