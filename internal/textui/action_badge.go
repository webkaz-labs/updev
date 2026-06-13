package textui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const defaultActionBadgeMaxWidth = 18

const (
	actionBadgeMarker        = "▶"
	actionBadgeBackendToken  = "bak"
	actionBadgeUpdateToken   = "upd"
	actionBadgeUpdatedToken  = "up"
	actionBadgeHoldToken     = "hold"
	actionBadgeSecurityToken = "sec"
	actionBadgeManualToken   = "man"
	actionBadgeFilterToken   = "flt"
)

var nerdFontHintCache = struct {
	sync.Mutex
	key     string
	ready   bool
	enabled bool
}{}

type ActionBadgeInput struct {
	Label  string
	Badge  string
	Status string
}

func ActionBadge(actions []ActionBadgeInput, color bool) string {
	return ActionBadgeWithWidth(actions, defaultActionBadgeMaxWidth, color)
}

func ActionBadgeWithWidth(actions []ActionBadgeInput, maxWidth int, color bool) string {
	labels := []actionBadgeEntry{}
	seen := map[string]bool{}
	for _, action := range actions {
		label := compactAction(action)
		if label.Text == "" || seen[label.Text] {
			continue
		}
		seen[label.Text] = true
		labels = append(labels, label)
	}
	if len(labels) == 0 {
		return ""
	}
	sort.SliceStable(labels, func(i int, j int) bool {
		return actionBadgePriority(labels[i].Text) < actionBadgePriority(labels[j].Text)
	})
	return joinActionBadges(labels, maxWidth, color)
}

type actionBadgeEntry struct {
	Text   string
	Status string
}

func compactAction(action ActionBadgeInput) actionBadgeEntry {
	if badge := strings.TrimSpace(action.Badge); badge != "" {
		return actionBadgeEntry{Text: compactActionCustomBadge(badge), Status: strings.TrimSpace(action.Status)}
	}
	return actionBadgeEntry{Text: compactActionLabel(action.Label)}
}

func compactActionCustomBadge(badge string) string {
	fields := strings.Fields(strings.TrimSpace(badge))
	if len(fields) == 0 {
		return ""
	}
	token := fields[0]
	if !knownActionBadgeToken(token) {
		return strings.TrimSpace(badge)
	}
	out := actionBadgeToken(token)
	if len(fields) > 1 {
		out += " " + strings.Join(fields[1:], " ")
	}
	return out
}

func knownActionBadgeToken(token string) bool {
	switch token {
	case actionBadgeBackendToken, actionBadgeUpdateToken, actionBadgeUpdatedToken, actionBadgeHoldToken, actionBadgeSecurityToken, actionBadgeManualToken, actionBadgeFilterToken:
		return true
	default:
		return false
	}
}

func compactActionLabel(label string) string {
	label = strings.TrimSpace(label)
	lower := strings.ToLower(label)
	switch {
	case strings.Contains(lower, "backend"):
		return actionBadgeToken(actionBadgeBackendToken)
	case strings.Contains(lower, "update"):
		return actionBadgeToken(actionBadgeUpdateToken)
	case strings.Contains(lower, "security"):
		return actionBadgeToken(actionBadgeSecurityToken)
	case strings.Contains(lower, "manual"):
		return actionBadgeToken(actionBadgeManualToken)
	case strings.Contains(lower, "filter"):
		return actionBadgeToken(actionBadgeFilterToken)
	case strings.Contains(label, "backend"):
		return actionBadgeToken(actionBadgeBackendToken)
	case strings.Contains(label, "更新"):
		return actionBadgeToken(actionBadgeUpdateToken)
	case strings.Contains(label, "security") || strings.Contains(label, "セキュリティ"):
		return actionBadgeToken(actionBadgeSecurityToken)
	case strings.Contains(label, "手動") || strings.Contains(label, "manual"):
		return actionBadgeToken(actionBadgeManualToken)
	}
	replacements := []struct {
		old string
		new string
	}{
		{"を開く", ""},
		{"を確認", ""},
		{"open ", ""},
		{"review ", ""},
	}
	for _, replacement := range replacements {
		label = strings.ReplaceAll(label, replacement.old, replacement.new)
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	return Truncate(label, 18)
}

func actionBadgePriority(label string) int {
	base := actionBadgeBaseToken(label)
	if fields := strings.Fields(base); len(fields) > 0 {
		base = fields[0]
	}
	switch base {
	case actionBadgeUpdatedToken:
		return 10
	case actionBadgeUpdateToken:
		return 15
	case actionBadgeSecurityToken:
		return 20
	case actionBadgeHoldToken:
		return 25
	case actionBadgeManualToken:
		return 30
	case actionBadgeBackendToken:
		return 40
	case actionBadgeFilterToken:
		return 50
	default:
		return 100
	}
}

func joinActionBadges(labels []actionBadgeEntry, maxWidth int, color bool) string {
	raw := ""
	styled := ""
	for index, label := range labels {
		candidateRaw := label.Text
		candidateStyled := styleActionBadge(label, color)
		if raw != "" {
			candidateRaw = raw + " " + label.Text
			candidateStyled = styled + " " + styleActionBadge(label, color)
		}
		remaining := len(labels) - index - 1
		if remaining > 0 {
			withOmitted := candidateRaw + fmt.Sprintf(" +%d", remaining)
			if DisplayWidth(withOmitted) > maxWidth {
				if raw == "" {
					return styleActionBadge(actionBadgeEntry{Text: Truncate(label.Text, maxWidth), Status: label.Status}, color)
				}
				return appendOmittedActionBadges(raw, styled, len(labels)-index, maxWidth)
			}
		} else if DisplayWidth(candidateRaw) > maxWidth {
			if raw == "" {
				return styleActionBadge(actionBadgeEntry{Text: Truncate(label.Text, maxWidth), Status: label.Status}, color)
			}
			return appendOmittedActionBadges(raw, styled, 1, maxWidth)
		}
		raw = candidateRaw
		styled = candidateStyled
	}
	return styled
}

func styleActionBadge(label actionBadgeEntry, color bool) string {
	if label.Status != "" {
		return StyleStatusText(label.Text, label.Status, color)
	}
	return StyleAction(label.Text, color)
}

func appendOmittedActionBadges(raw string, styled string, omitted int, maxWidth int) string {
	if omitted <= 0 {
		return styled
	}
	candidate := raw + fmt.Sprintf(" +%d", omitted)
	if DisplayWidth(candidate) <= maxWidth {
		return styled + fmt.Sprintf(" +%d", omitted)
	}
	return Truncate(styled, maxWidth)
}

func actionBadgeToken(ascii string) string {
	if ActionBadgeIconsEnabled() {
		return actionBadgeNerdMarker(ascii) + ascii
	}
	return actionBadgeMarker + ascii
}

func actionBadgeNerdMarker(token string) string {
	switch token {
	case actionBadgeSecurityToken:
		return "🔒"
	case actionBadgeUpdatedToken:
		return "✅"
	case actionBadgeHoldToken:
		return "⏳"
	case actionBadgeUpdateToken:
		return "🔄"
	case actionBadgeManualToken:
		return "📝"
	case actionBadgeBackendToken:
		return "📦"
	case actionBadgeFilterToken:
		return "🔎"
	default:
		return actionBadgeMarker
	}
}

func actionBadgeBaseToken(label string) string {
	label = strings.TrimSpace(label)
	label = strings.TrimPrefix(label, actionBadgeMarker)
	for _, marker := range actionBadgeNerdMarkers() {
		label = strings.TrimPrefix(label, marker)
	}
	return strings.TrimSpace(label)
}

func actionBadgeNerdMarkers() []string {
	return []string{
		actionBadgeNerdMarker(actionBadgeSecurityToken),
		actionBadgeNerdMarker(actionBadgeUpdatedToken),
		actionBadgeNerdMarker(actionBadgeHoldToken),
		actionBadgeNerdMarker(actionBadgeUpdateToken),
		actionBadgeNerdMarker(actionBadgeManualToken),
		actionBadgeNerdMarker(actionBadgeBackendToken),
		actionBadgeNerdMarker(actionBadgeFilterToken),
	}
}

func ActionBadgeIconsEnabled() bool {
	if value, ok := envSetting("UPDEV_NERD_FONT"); ok {
		return nerdFontSettingEnabled(value)
	}
	if value, ok := envSetting("NERD_FONT"); ok {
		return nerdFontSettingEnabled(value)
	}
	return detectNerdFontHint()
}

func nerdFontSettingEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "auto":
		return detectNerdFontHint()
	default:
		return false
	}
}

func envSetting(name string) (string, bool) {
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func detectNerdFontHint() bool {
	key := strings.Join([]string{
		os.Getenv("TERM"),
		os.Getenv("WEZTERM_CONFIG_FILE"),
		os.Getenv("XDG_CONFIG_HOME"),
		os.Getenv("HOME"),
	}, "\x00")
	nerdFontHintCache.Lock()
	if nerdFontHintCache.ready && nerdFontHintCache.key == key {
		enabled := nerdFontHintCache.enabled
		nerdFontHintCache.Unlock()
		return enabled
	}
	nerdFontHintCache.Unlock()

	enabled := detectNerdFontHintUncached()

	nerdFontHintCache.Lock()
	nerdFontHintCache.key = key
	nerdFontHintCache.ready = true
	nerdFontHintCache.enabled = enabled
	nerdFontHintCache.Unlock()
	return enabled
}

func detectNerdFontHintUncached() bool {
	term := strings.TrimSpace(os.Getenv("TERM"))
	if term == "" || term == "dumb" {
		return false
	}
	for _, path := range terminalConfigPaths() {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if configMentionsNerdFont(string(data)) {
			return true
		}
	}
	return false
}

func terminalConfigPaths() []string {
	paths := []string{}
	add := func(path string) {
		if strings.TrimSpace(path) != "" {
			paths = append(paths, path)
		}
	}
	add(os.Getenv("WEZTERM_CONFIG_FILE"))
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			configHome = filepath.Join(home, ".config")
		}
	}
	if configHome != "" {
		add(filepath.Join(configHome, "wezterm", "wezterm.lua"))
		add(filepath.Join(configHome, "ghostty", "config"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, "Library", "Application Support", "com.mitchellh.ghostty", "config"))
	}
	return paths
}

func configMentionsNerdFont(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "nerd") || strings.Contains(value, " NF")
}
