package brew

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
)

type TrustTarget struct {
	Kind             string   `json:"kind"`
	Name             string   `json:"name"`
	Tap              string   `json:"tap"`
	Trusted          bool     `json:"trusted"`
	TrustSource      string   `json:"trust_source,omitempty"`
	TrustCommand     string   `json:"trust_command"`
	TrustCommandArgv []string `json:"trust_command_argv,omitempty"`
	Source           string   `json:"source,omitempty"`
}

type TrustGroup struct {
	Tap              string        `json:"tap"`
	Trusted          bool          `json:"trusted"`
	TrustedCount     int           `json:"trusted_count"`
	UntrustedCount   int           `json:"untrusted_count"`
	TargetCount      int           `json:"target_count"`
	TrustCommand     string        `json:"trust_command,omitempty"`
	TrustCommandArgv []string      `json:"trust_command_argv,omitempty"`
	Targets          []TrustTarget `json:"targets,omitempty"`
}

type TrustState struct {
	Taps     []string `json:"taps"`
	Formulae []string `json:"formulae"`
	Casks    []string `json:"casks"`
	Commands []string `json:"commands"`
}

func ParseTrustTargets(reader io.Reader, source string) ([]TrustTarget, error) {
	lineRe := regexp.MustCompile(`^\s*(brew|cask|tap)\s+"([^"]+)"`)
	targets := []TrustTarget{}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		match := lineRe.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		kind := match[1]
		rawName := strings.TrimSpace(match[2])
		if rawName == "" || IsURLName(rawName) {
			continue
		}
		var target TrustTarget
		switch kind {
		case "tap":
			if IsOfficialTap(rawName) {
				continue
			}
			target = TrustTarget{
				Kind:             "tap",
				Name:             rawName,
				Tap:              rawName,
				TrustCommand:     JoinCommand(TrustCommandArgv("tap", rawName)),
				TrustCommandArgv: TrustCommandArgv("tap", rawName),
				Source:           source,
			}
		case "brew", "cask":
			tap := TapName(kind, rawName)
			if tap == "" || IsOfficialTap(tap) {
				continue
			}
			trustKind := "formula"
			if kind == "cask" {
				trustKind = "cask"
			}
			target = TrustTarget{
				Kind:             trustKind,
				Name:             rawName,
				Tap:              tap,
				TrustCommand:     JoinCommand(TrustCommandArgv(trustKind, rawName)),
				TrustCommandArgv: TrustCommandArgv(trustKind, rawName),
				Source:           source,
			}
		}
		key := target.Kind + "\x00" + target.Name
		if target.Name == "" || seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, target)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Kind != targets[j].Kind {
			return targets[i].Kind < targets[j].Kind
		}
		return targets[i].Name < targets[j].Name
	})
	return targets, scanner.Err()
}

func ParseTrustState(raw string) (TrustState, error) {
	payload := strings.TrimSpace(raw)
	if payload == "" {
		return TrustState{}, fmt.Errorf("empty trust JSON")
	}
	start := strings.Index(payload, "{")
	end := strings.LastIndex(payload, "}")
	if start < 0 || end < start {
		return TrustState{}, fmt.Errorf("no JSON object")
	}
	var state TrustState
	if err := json.Unmarshal([]byte(payload[start:end+1]), &state); err != nil {
		return TrustState{}, err
	}
	return state, nil
}

func ApplyTrustState(targets []TrustTarget, state TrustState) []TrustTarget {
	taps := stringSet(state.Taps)
	formulae := stringSet(state.Formulae)
	casks := stringSet(state.Casks)
	out := append([]TrustTarget(nil), targets...)
	for i := range out {
		switch out[i].Kind {
		case "tap":
			if taps[out[i].Name] {
				out[i].Trusted = true
				out[i].TrustSource = "tap"
			}
		case "formula":
			if formulae[out[i].Name] {
				out[i].Trusted = true
				out[i].TrustSource = "formula"
			} else if taps[out[i].Tap] {
				out[i].Trusted = true
				out[i].TrustSource = "tap"
			}
		case "cask":
			if casks[out[i].Name] {
				out[i].Trusted = true
				out[i].TrustSource = "cask"
			} else if taps[out[i].Tap] {
				out[i].Trusted = true
				out[i].TrustSource = "tap"
			}
		}
	}
	return out
}

func TrustTargetCounts(targets []TrustTarget) (int, int) {
	trusted := 0
	untrusted := 0
	for _, target := range targets {
		if target.Trusted {
			trusted++
		} else {
			untrusted++
		}
	}
	return trusted, untrusted
}

func TrustGroupsByTap(targets []TrustTarget) []TrustGroup {
	byTap := map[string][]TrustTarget{}
	for _, target := range targets {
		tap := strings.TrimSpace(target.Tap)
		if tap == "" {
			tap = strings.TrimSpace(target.Name)
		}
		if tap == "" {
			continue
		}
		byTap[tap] = append(byTap[tap], target)
	}
	taps := make([]string, 0, len(byTap))
	for tap := range byTap {
		taps = append(taps, tap)
	}
	sort.Strings(taps)
	groups := make([]TrustGroup, 0, len(taps))
	for _, tap := range taps {
		targets := append([]TrustTarget(nil), byTap[tap]...)
		sort.Slice(targets, func(i, j int) bool {
			if targets[i].Kind != targets[j].Kind {
				return targets[i].Kind < targets[j].Kind
			}
			return targets[i].Name < targets[j].Name
		})
		group := TrustGroup{
			Tap:              tap,
			TrustCommand:     JoinCommand(TrustCommandArgv("tap", tap)),
			TrustCommandArgv: TrustCommandArgv("tap", tap),
			Targets:          targets,
			TargetCount:      len(targets),
		}
		for _, target := range targets {
			if target.Trusted {
				group.TrustedCount++
			} else {
				group.UntrustedCount++
			}
		}
		group.Trusted = group.UntrustedCount == 0
		groups = append(groups, group)
	}
	return groups
}

func TrustGroupCounts(groups []TrustGroup) (int, int) {
	trusted := 0
	untrusted := 0
	for _, group := range groups {
		if group.Trusted {
			trusted++
		} else {
			untrusted++
		}
	}
	return trusted, untrusted
}

func UntrustedTargetNames(targets []TrustTarget, limit int) []string {
	names := []string{}
	for _, target := range targets {
		if target.Trusted {
			continue
		}
		names = append(names, target.Kind+" "+target.Name)
		if limit > 0 && len(names) == limit {
			break
		}
	}
	remaining := 0
	for _, target := range targets {
		if !target.Trusted {
			remaining++
		}
	}
	if limit > 0 && remaining > len(names) {
		names = append(names, fmt.Sprintf("...%d more", remaining-len(names)))
	}
	return names
}

func UntrustedGroupNames(groups []TrustGroup, limit int) []string {
	names := []string{}
	for _, group := range groups {
		if group.Trusted {
			continue
		}
		names = append(names, fmt.Sprintf("%s (%d untrusted/%d targets)", group.Tap, group.UntrustedCount, group.TargetCount))
		if limit > 0 && len(names) == limit {
			break
		}
	}
	remaining := 0
	for _, group := range groups {
		if !group.Trusted {
			remaining++
		}
	}
	if limit > 0 && remaining > len(names) {
		names = append(names, fmt.Sprintf("...%d more", remaining-len(names)))
	}
	return names
}

func FirstUntrustedTrustCommand(groups []TrustGroup) string {
	for _, group := range groups {
		if !group.Trusted && strings.TrimSpace(group.TrustCommand) != "" {
			return group.TrustCommand
		}
	}
	return ""
}

func ValidTrustTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" || strings.HasPrefix(target, "-") || IsURLName(target) {
		return false
	}
	return !strings.ContainsAny(target, "\t\r\n")
}

func stringSet(values []string) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = true
		}
	}
	return set
}
