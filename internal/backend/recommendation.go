package backend

import (
	"fmt"
	"strings"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

func homebrewRecommendationConfidence(recommendation Recommendation, alreadyDesired bool) string {
	if RecommendationKind(recommendation) == "candidate" {
		if recommendation.AssetEvidence.Status == "compatible" {
			return "medium"
		}
		return "low"
	}
	if alreadyDesired {
		return "high"
	}
	return "medium"
}

func miseRecommendationConfidence(spec string, recommendation Recommendation, alreadyDesired bool) string {
	if RecommendationKind(recommendation) == "candidate" {
		if recommendation.AssetEvidence.Status == "compatible" {
			return "medium"
		}
		return "low"
	}
	if alreadyDesired && backendSpecHasOSConditions(spec) {
		return "high"
	}
	if alreadyDesired {
		return "medium"
	}
	return "low"
}

func homebrewRecommendationAction(item plan.Item, recommendation Recommendation, alreadyDesired bool) string {
	if RecommendationKind(recommendation) == "candidate" {
		return fmt.Sprintf("review %s as a candidate only; verify release assets, version mapping, and ownership before changing Homebrew ownership", recommendation.Name)
	}
	if alreadyDesired {
		return fmt.Sprintf("review why %s remains in Brewfile before removing; keep it if bootstrap or cask dependency requires Homebrew", item.Name)
	}
	return fmt.Sprintf("review adding %s to mise before considering Brewfile removal", recommendation.Name)
}

func miseRecommendationAction(name string, spec string, recommendedSpec string, recommendation Recommendation, alreadyDesired bool) string {
	if RecommendationKind(recommendation) == "candidate" {
		sourceBackend := backendSourceNamePrefix(name)
		switch recommendation.AssetEvidence.Status {
		case "compatible":
			return fmt.Sprintf("review %s as a candidate; release assets appear to match %s, but verify version mapping and official distribution before replacing %s", recommendation.Name, recommendation.AssetEvidence.Platform, name)
		case "no-assets", "no-match":
			if sourceBackend == "cargo" {
				return fmt.Sprintf("keep %s as a local cargo build unless compatible GitHub release assets for %s are verified on %s", name, recommendation.Name, recommendation.AssetEvidence.Platform)
			}
			if sourceBackend == "npm" {
				return fmt.Sprintf("keep %s unless compatible GitHub release assets for %s are verified on %s and npm is not the official distribution path", name, recommendation.Name, recommendation.AssetEvidence.Platform)
			}
			return fmt.Sprintf("keep %s unless compatible GitHub release assets for %s are verified on %s", name, recommendation.Name, recommendation.AssetEvidence.Platform)
		default:
			return fmt.Sprintf("review %s as a candidate only; verify release assets, version mapping, and official distribution before replacing %s", recommendation.Name, name)
		}
	}
	if alreadyDesired {
		if backendOSSelectorsCovered(backendOSConditions(spec), backendOSConditions(recommendedSpec)) {
			return fmt.Sprintf("preferred entry %s already covers %s; remove the old key after confirmation", recommendation.Name, name)
		}
		return fmt.Sprintf("preferred entry %s already exists; preserve OS conditions before removing or narrowing %s", recommendation.Name, name)
	}
	if backendSpecHasOSConditions(spec) {
		return fmt.Sprintf("review OS-specific conditions before replacing %s with %s; copy current os list to the preferred entry", name, recommendation.Name)
	}
	return fmt.Sprintf("review replacing %s with %s", name, recommendation.Name)
}

func backendSourceNamePrefix(name string) string {
	prefix, _, ok := strings.Cut(name, ":")
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(prefix))
}

func backendCommandStatus(commandRunner runner.Runner, commands []string) string {
	if len(commands) == 0 {
		return "unknown"
	}
	missing := 0
	for _, command := range commands {
		if _, err := commandRunner.LookPath(command); err != nil {
			missing++
		}
	}
	switch missing {
	case 0:
		return "on-path"
	case len(commands):
		return "missing"
	default:
		return "partial"
	}
}

func backendOSConditions(spec string) []string {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil
	}
	keyIndex := strings.Index(spec, "os")
	if keyIndex < 0 {
		return nil
	}
	assignment := spec[keyIndex:]
	start := strings.Index(assignment, "[")
	if start < 0 {
		return nil
	}
	end := strings.Index(assignment[start:], "]")
	if end < 0 {
		return nil
	}
	rawList := assignment[start+1 : start+end]
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(rawList, ",") {
		token := strings.Trim(strings.TrimSpace(raw), `"'`)
		if token == "" || seen[token] {
			continue
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

func backendSpecHasOSConditions(spec string) bool {
	return len(backendOSConditions(spec)) > 0
}

func backendOSSelectorsCovered(current []string, recommended []string) bool {
	currentOS := normalizedBackendOSSet(current)
	recommendedOS := normalizedBackendOSSet(recommended)
	if len(currentOS) == 0 {
		return len(recommendedOS) == 0
	}
	if len(recommendedOS) == 0 {
		return true
	}
	for osName := range currentOS {
		if !recommendedOS[osName] {
			return false
		}
	}
	return true
}

func normalizedBackendOSSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		out[value] = true
	}
	return out
}
