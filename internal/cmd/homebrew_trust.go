package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

type homebrewTrustTarget struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Tap          string `json:"tap"`
	Trusted      bool   `json:"trusted"`
	TrustSource  string `json:"trust_source,omitempty"`
	TrustCommand string `json:"trust_command"`
	Source       string `json:"source,omitempty"`
}

type homebrewTrustState struct {
	Taps     []string `json:"taps"`
	Formulae []string `json:"formulae"`
	Casks    []string `json:"casks"`
	Commands []string `json:"commands"`
}

func homebrewTapTrustDependencyCheck(ctx context.Context, commandRunner runner.Runner, root string) dependencyContractCheck {
	check := dependencyContractCheck{
		Tool:     "brew",
		Feature:  "tap-trust",
		Required: false,
		Command:  []string{"env", "HOMEBREW_NO_INSTALL_FROM_API=1", "brew", "trust", "--json=v1"},
		Status:   plan.StatusOK,
	}
	if _, err := commandRunner.LookPath("brew"); err != nil {
		check.Status = plan.StatusUnavailable
		check.Reason = "executable not found on PATH"
		check.Remediation = dependencyRemediation("brew", true)
		return check
	}
	targets, err := homebrewTrustTargetsFromRoot(root)
	if err != nil {
		check.Status = plan.StatusDrift
		check.Reason = "could not inspect Brewfile trust targets: " + err.Error()
		check.Remediation = "verify the configured root and Brewfile path before relying on Homebrew tap trust diagnostics"
		return check
	}
	if len(targets) == 0 {
		check.Value = "no non-official Brewfile trust targets"
		return check
	}
	result := runDependencyCommand(ctx, commandRunner, "env", "HOMEBREW_NO_INSTALL_FROM_API=1", "brew", "trust", "--json=v1")
	state, err := parseHomebrewTrustState(result.Stdout)
	if err != nil {
		check.Status = plan.StatusDrift
		check.Reason = "brew trust --json=v1 output is not valid JSON"
		if detail := dependencyCommandError(result); detail != "" {
			check.Reason += ": " + detail
		}
		check.Remediation = "upgrade or repair Homebrew tap trust support; updev expects brew trust --json=v1"
		return check
	}
	targets = applyHomebrewTrustState(targets, state)
	check.TrustTargets = targets
	trusted, untrusted := homebrewTrustTargetCounts(targets)
	check.Value = fmt.Sprintf("%d trusted, %d untrusted, %d targets", trusted, untrusted, len(targets))
	if result.Code != 0 || result.Err != nil {
		check.Status = plan.StatusDrift
		check.Reason = "brew trust --json=v1 returned non-zero but JSON output was parsed"
		check.Remediation = "repair Homebrew trust metadata access; updev used the parsed trust JSON but provider diagnostics should exit cleanly"
		return check
	}
	if untrusted > 0 {
		check.Status = plan.StatusDrift
		check.Reason = "untrusted Homebrew tap targets: " + strings.Join(homebrewUntrustedTargetNames(targets, 4), ", ")
		check.Remediation = "review source provenance, then prefer item-scoped brew trust commands"
		if command := firstHomebrewUntrustedTrustCommand(targets); command != "" {
			check.Remediation += " such as `" + command + "`"
		}
		check.Remediation += "; trust whole taps only when you accept all current and future entries"
	}
	return check
}

func homebrewTrustTargetsFromRoot(root string) ([]homebrewTrustTarget, error) {
	path := homebrewTrustBrewfilePath(root)
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	return parseHomebrewTrustTargets(file, path)
}

func homebrewTrustBrewfilePath(root string) string {
	return filepath.Join(root, "Brewfile.tmpl")
}

func parseHomebrewTrustTargets(reader io.Reader, source string) ([]homebrewTrustTarget, error) {
	lineRe := regexp.MustCompile(`^\s*(brew|cask|tap)\s+"([^"]+)"`)
	targets := []homebrewTrustTarget{}
	seen := map[string]bool{}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		match := lineRe.FindStringSubmatch(scanner.Text())
		if match == nil {
			continue
		}
		kind := match[1]
		rawName := strings.TrimSpace(match[2])
		if rawName == "" || isURLBrewName(rawName) {
			continue
		}
		var target homebrewTrustTarget
		switch kind {
		case "tap":
			if isOfficialBrewTap(rawName) {
				continue
			}
			target = homebrewTrustTarget{
				Kind:         "tap",
				Name:         rawName,
				Tap:          rawName,
				TrustCommand: "brew trust --tap " + rawName,
				Source:       source,
			}
		case "brew", "cask":
			tap := brewSafetyTap(kind, rawName)
			if tap == "" || isOfficialBrewTap(tap) {
				continue
			}
			trustKind := "formula"
			if kind == "cask" {
				trustKind = "cask"
			}
			target = homebrewTrustTarget{
				Kind:         trustKind,
				Name:         rawName,
				Tap:          tap,
				TrustCommand: "brew trust --" + trustKind + " " + rawName,
				Source:       source,
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

func parseHomebrewTrustState(raw string) (homebrewTrustState, error) {
	payload := strings.TrimSpace(raw)
	if payload == "" {
		return homebrewTrustState{}, fmt.Errorf("empty trust JSON")
	}
	start := strings.Index(payload, "{")
	end := strings.LastIndex(payload, "}")
	if start < 0 || end < start {
		return homebrewTrustState{}, fmt.Errorf("no JSON object")
	}
	var state homebrewTrustState
	if err := json.Unmarshal([]byte(payload[start:end+1]), &state); err != nil {
		return homebrewTrustState{}, err
	}
	return state, nil
}

func applyHomebrewTrustState(targets []homebrewTrustTarget, state homebrewTrustState) []homebrewTrustTarget {
	taps := stringSet(state.Taps)
	formulae := stringSet(state.Formulae)
	casks := stringSet(state.Casks)
	out := append([]homebrewTrustTarget(nil), targets...)
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

func homebrewTrustTargetCounts(targets []homebrewTrustTarget) (int, int) {
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

func homebrewUntrustedTargetNames(targets []homebrewTrustTarget, limit int) []string {
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

func firstHomebrewUntrustedTrustCommand(targets []homebrewTrustTarget) string {
	for _, target := range targets {
		if !target.Trusted && strings.TrimSpace(target.TrustCommand) != "" {
			return target.TrustCommand
		}
	}
	return ""
}
