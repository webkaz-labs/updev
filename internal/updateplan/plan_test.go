package updateplan

import (
	"reflect"
	"testing"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/updatereason"
	"github.com/webkaz-labs/updev/internal/updatereport"
)

func TestDefaultStepsUseProviderCommandPlans(t *testing.T) {
	steps := DefaultSteps()
	if len(steps) != 2 {
		t.Fatalf("expected brew and mise steps, got %#v", steps)
	}
	if !reflect.DeepEqual(steps[0].Commands, []updatereport.Command{
		{Command: []string{"brew", "update"}},
		{Command: []string{"brew", "upgrade", "--greedy"}},
		{Command: []string{"brew", "cleanup"}},
	}) {
		t.Fatalf("unexpected brew plan: %#v", steps[0].Commands)
	}
	if !reflect.DeepEqual(steps[1].Commands, []updatereport.Command{
		{Command: []string{"mise", "upgrade"}},
		{Command: []string{"mise", "prune"}},
	}) {
		t.Fatalf("unexpected mise plan: %#v", steps[1].Commands)
	}
	if step, ok := StepForProvider("mise"); !ok || step.Name != "mise" {
		t.Fatalf("expected mise step lookup, got %#v, %v", step, ok)
	}
}

func TestScopeStrictBuildsItemScopedBrewPlan(t *testing.T) {
	step, _ := StepForProvider("brew")
	result := ScopeStrict(step, StrictOptions{Enabled: true}, []securitygate.Gate{{
		Provider: "brew",
		Status:   plan.StatusHeld,
		Findings: []securitygate.Finding{
			{Name: "zeta", Decision: "allow"},
			{Name: "alpha", Decision: "allow"},
			{Name: "zeta", Decision: "allow"},
			{Name: "held", Decision: "hold"},
		},
	}})

	want := []updatereport.Command{
		{Command: []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "upgrade", "--greedy", "alpha", "zeta"}},
		{Command: []string{"env", "HOMEBREW_NO_AUTO_UPDATE=1", "brew", "cleanup"}},
		{Command: []string{"brew", "update"}},
	}
	if !reflect.DeepEqual(result.Step.Commands, want) {
		t.Fatalf("unexpected scoped brew plan:\nwant: %#v\ngot:  %#v", want, result.Step.Commands)
	}
	if result.Step.ReasonCode != updatereason.StrictBrewPartial || len(result.SkippedFindings) != 1 || result.HoldReason.Code != "" {
		t.Fatalf("unexpected strict brew result: %#v", result)
	}
}

func TestScopeStrictBuildsItemScopedMisePlan(t *testing.T) {
	step, _ := StepForProvider("mise")
	result := ScopeStrict(step, StrictOptions{
		Enabled:               true,
		Root:                  "/repo",
		MiseMinimumReleaseAge: "5d",
	}, []securitygate.Gate{{
		Provider: "mise",
		Status:   plan.StatusOK,
		Findings: []securitygate.Finding{{Name: "github:openai/codex", Decision: "allow"}},
	}})

	want := []string{"mise", "upgrade", "--yes", "--minimum-release-age", "5d", "--cd", "/repo", "github:openai/codex"}
	if !reflect.DeepEqual(result.Step.Command, want) {
		t.Fatalf("unexpected scoped mise command:\nwant: %#v\ngot:  %#v", want, result.Step.Command)
	}
	if len(result.Step.Commands) != 2 || !reflect.DeepEqual(result.Step.Commands[1].Command, []string{"mise", "prune"}) {
		t.Fatalf("unexpected scoped mise plan: %#v", result.Step.Commands)
	}
}

func TestScopeStrictPlansBrewRefreshWhenNoCandidates(t *testing.T) {
	step, _ := StepForProvider("brew")
	result := ScopeStrict(step, StrictOptions{Enabled: true}, []securitygate.Gate{{Provider: "brew", Status: plan.StatusOK}})

	if !IsBrewRefreshOnly(result.Step) {
		t.Fatalf("expected refresh-only brew step, got %#v", result.Step)
	}
	if result.Step.ReasonCode != updatereason.StrictBrewRefreshOnly {
		t.Fatalf("expected refresh-only reason, got %#v", result.Step)
	}
}

func TestScopeStrictHoldsFailedOrUnapprovedProvider(t *testing.T) {
	step, _ := StepForProvider("mise")
	failed := ScopeStrict(step, StrictOptions{Enabled: true}, []securitygate.Gate{{
		Provider: "mise",
		Status:   plan.StatusError,
		Error:    "scanner failed",
	}})
	if failed.HoldReason.Code != updatereason.StrictGateFailed || failed.Step.ReasonCode != updatereason.StrictGateFailed {
		t.Fatalf("expected failed gate hold, got %#v", failed)
	}

	held := ScopeStrict(step, StrictOptions{Enabled: true}, []securitygate.Gate{{
		Provider: "mise",
		Status:   plan.StatusHeld,
		Findings: []securitygate.Finding{{Name: "node", Decision: "review"}},
	}})
	if held.HoldReason.Code != updatereason.StrictGateReview || len(held.SkippedFindings) != 0 {
		t.Fatalf("expected review hold, got %#v", held)
	}
}

func TestScopeStrictLeavesNonStrictAndUnknownProvidersUnchanged(t *testing.T) {
	step, _ := StepForProvider("brew")
	result := ScopeStrict(step, StrictOptions{}, []securitygate.Gate{{Provider: "brew", Status: plan.StatusHeld}})
	if !reflect.DeepEqual(result.Step, step) || result.HoldReason.Code != "" {
		t.Fatalf("expected non-strict plan unchanged, got %#v", result)
	}

	unknown := updatereport.Step{Name: "other", Command: []string{"other", "update"}}
	result = ScopeStrict(unknown, StrictOptions{Enabled: true}, []securitygate.Gate{{
		Provider: "other",
		Status:   plan.StatusHeld,
		Findings: []securitygate.Finding{{Name: "item", Decision: "allow"}},
	}})
	if result.HoldReason.Code != updatereason.StrictGateReview {
		t.Fatalf("expected unknown held provider to require review, got %#v", result)
	}
}
