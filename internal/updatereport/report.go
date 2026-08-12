package updatereport

import (
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/updatelog"
)

type Command struct {
	Command []string `json:"command"`
}

type Step struct {
	Name         string            `json:"name"`
	Command      []string          `json:"command"`
	Commands     []Command         `json:"commands,omitempty"`
	Status       plan.Status       `json:"status"`
	Stdout       string            `json:"stdout,omitempty"`
	Stderr       string            `json:"stderr,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	ReasonCode   string            `json:"reason_code,omitempty"`
	ReasonArgs   map[string]string `json:"reason_args,omitempty"`
	Skipped      bool              `json:"skipped,omitempty"`
	Updated      []string          `json:"updated,omitempty"`
	SkippedItems []string          `json:"skipped_items,omitempty"`
}

type Report[P any] struct {
	Status    plan.Status         `json:"status"`
	Root      string              `json:"root"`
	DryRun    bool                `json:"dry_run"`
	Security  string              `json:"security"`
	Policy    *P                  `json:"policy,omitempty"`
	Warnings  []string            `json:"warnings,omitempty"`
	Steps     []Step              `json:"steps"`
	Safety    []securitygate.Gate `json:"safety,omitempty"`
	Inventory plan.Report         `json:"inventory"`
	Report    string              `json:"report,omitempty"`
}

type CacheEntry[P any] struct {
	Version   int       `json:"version"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Report    Report[P] `json:"report"`
}

type SectionView[P any] struct {
	Version   int                 `json:"version"`
	Type      string              `json:"type"`
	CreatedAt time.Time           `json:"created_at"`
	Section   string              `json:"section"`
	Status    plan.Status         `json:"status"`
	Filters   map[string]string   `json:"filters,omitempty"`
	Summary   ViewSummary         `json:"summary"`
	Report    *Report[P]          `json:"report,omitempty"`
	Steps     []Step              `json:"steps,omitempty"`
	Safety    []securitygate.Gate `json:"safety,omitempty"`
	Inventory *plan.Report        `json:"inventory,omitempty"`
}

type ViewSummary struct {
	Steps              int `json:"steps"`
	SkippedSteps       int `json:"skipped_steps,omitempty"`
	HeldSteps          int `json:"held_steps,omitempty"`
	ErrorSteps         int `json:"error_steps,omitempty"`
	SafetyGates        int `json:"safety_gates,omitempty"`
	SafetyAttention    int `json:"safety_attention,omitempty"`
	InventoryItems     int `json:"inventory_items,omitempty"`
	InventoryAttention int `json:"inventory_attention,omitempty"`
}

type Filter struct {
	Provider string
	Status   string
	Query    string
}

type InventoryFilter func(plan.Report, Filter) plan.Report

func New[P any](root string, dryRun bool, security string, policy *P) Report[P] {
	return Report[P]{
		Status:   plan.StatusOK,
		Root:     root,
		DryRun:   dryRun,
		Security: security,
		Policy:   policy,
	}
}

func (report *Report[P]) AppendStep(step Step) {
	report.Steps = append(report.Steps, step)
}

func (report *Report[P]) ReplaceOrAppendStep(step Step) {
	for index, existing := range report.Steps {
		if existing.Name == step.Name {
			report.Steps[index] = step
			return
		}
	}
	report.Steps = append(report.Steps, step)
}

func (report *Report[P]) ReplaceOrAppendGate(gate securitygate.Gate) {
	for index, existing := range report.Safety {
		if existing.Provider == gate.Provider {
			report.Safety[index] = gate
			return
		}
	}
	report.Safety = append(report.Safety, gate)
}

func (report *Report[P]) SetInventory(inventory plan.Report) {
	report.Inventory = inventory
}

func (report *Report[P]) RecomputeStatus(includeSafety bool) {
	status := stepsStatus(report.Steps)
	if report.Inventory.Status == plan.StatusError {
		report.Status = plan.StatusError
		return
	}
	if includeSafety {
		status = reduceStepStatus(status, gatesStatus(report.Safety))
	}
	report.Status = status
}

func reduceStepStatus(current plan.Status, candidate plan.Status) plan.Status {
	if current == "" {
		current = plan.StatusOK
	}
	switch candidate {
	case plan.StatusError:
		return plan.StatusError
	case plan.StatusBlocked:
		if current != plan.StatusError {
			return plan.StatusBlocked
		}
	case plan.StatusHeld:
		if current != plan.StatusError && current != plan.StatusBlocked {
			return plan.StatusHeld
		}
	case plan.StatusDrift, plan.StatusMissing, plan.StatusExtra:
		if current == plan.StatusOK {
			return plan.StatusDrift
		}
	}
	return current
}

func stepsStatus(steps []Step) plan.Status {
	status := plan.StatusOK
	for _, step := range steps {
		status = reduceStepStatus(status, step.Status)
		if status == plan.StatusError {
			return status
		}
	}
	return status
}

func gatesStatus(gates []securitygate.Gate) plan.Status {
	status := plan.StatusOK
	for _, gate := range gates {
		switch gate.Status {
		case plan.StatusError:
			return plan.StatusError
		case plan.StatusBlocked:
			status = plan.StatusBlocked
		case plan.StatusHeld:
			if status != plan.StatusBlocked {
				status = plan.StatusHeld
			}
		}
	}
	return status
}

func SectionStatus[P any](report Report[P], section string) plan.Status {
	switch section {
	case "updates", "logs":
		return stepsStatus(report.Steps)
	case "security":
		return gatesStatus(report.Safety)
	case "inventory":
		if report.Inventory.Status != "" {
			return report.Inventory.Status
		}
		return plan.StatusOK
	default:
		if report.Status != "" {
			return report.Status
		}
		return plan.StatusOK
	}
}

func Summary[P any](report Report[P]) ViewSummary {
	summary := ViewSummary{
		Steps:          len(report.Steps),
		SafetyGates:    len(report.Safety),
		InventoryItems: len(report.Inventory.Items),
	}
	for _, step := range report.Steps {
		if step.Skipped {
			summary.SkippedSteps++
		}
		switch step.Status {
		case plan.StatusHeld:
			summary.HeldSteps++
		case plan.StatusError:
			summary.ErrorSteps++
		}
	}
	for _, gate := range report.Safety {
		if gate.Status == plan.StatusError || gate.Status == plan.StatusHeld || gate.Status == plan.StatusBlocked {
			summary.SafetyAttention++
			continue
		}
		for _, finding := range gate.Findings {
			if securitygate.DecisionNeedsAttention(finding.Decision) {
				summary.SafetyAttention++
				break
			}
		}
	}
	for _, item := range report.Inventory.Items {
		if plan.IsAttentionStatus(item.Status) {
			summary.InventoryAttention++
		}
	}
	return summary
}

func normalizeSteps(steps []Step) []Step {
	out := make([]Step, 0, len(steps))
	for _, step := range steps {
		normalizedUpdated := []string{}
		for _, item := range step.Updated {
			item = updatelog.NormalizeUpdatedItem(item)
			if item == "" || updatelog.IsProgressLine(item) {
				continue
			}
			normalizedUpdated = updatelog.AppendUniqueUpdated(normalizedUpdated, item)
		}
		normalizedSkipped := []string{}
		for _, item := range step.SkippedItems {
			item = updatelog.NormalizeSkippedItem(item)
			if item == "" || updatelog.IsGenericSkippedLine(item) {
				continue
			}
			normalizedSkipped = updatelog.AppendUniqueSkipped(normalizedSkipped, item)
		}
		step.Updated = normalizedUpdated
		step.SkippedItems = normalizedSkipped
		out = append(out, step)
	}
	return out
}

func Normalize[P any](report Report[P]) Report[P] {
	report.Steps = normalizeSteps(report.Steps)
	return report
}

func FilterReport[P any](report Report[P], filter Filter, filterInventory InventoryFilter) Report[P] {
	report = Normalize(report)
	report.Steps = filterSteps(report.Steps, filter)
	report.Safety = filterGates(report.Safety, filter)
	if filterInventory != nil {
		report.Inventory = filterInventory(report.Inventory, filter)
	}
	return report
}

func BuildSectionView[P any](entry CacheEntry[P], section string, filters map[string]string, report Report[P]) SectionView[P] {
	view := SectionView[P]{
		Version:   entry.Version,
		Type:      entry.Type,
		CreatedAt: entry.CreatedAt,
		Section:   section,
		Status:    SectionStatus(report, section),
		Filters:   filters,
		Summary:   Summary(report),
	}
	switch section {
	case "updates", "logs":
		view.Steps = report.Steps
	case "security":
		view.Safety = report.Safety
	case "inventory":
		inventory := report.Inventory
		view.Inventory = &inventory
	default:
		view.Report = &report
	}
	return view
}

func filterSteps(steps []Step, filter Filter) []Step {
	if filter.Provider == "" && filter.Status == "" && filter.Query == "" {
		return steps
	}
	out := make([]Step, 0, len(steps))
	for _, step := range steps {
		if filter.Provider != "" && !strings.EqualFold(step.Name, filter.Provider) {
			continue
		}
		if filter.Status != "" && !plan.StatusMatches(step.Status, filter.Status) {
			continue
		}
		if filter.Query != "" {
			filtered, ok := filterStepByQuery(step, filter.Query)
			if !ok {
				continue
			}
			step = filtered
		}
		out = append(out, step)
	}
	return out
}

func filterStepByQuery(step Step, query string) (Step, bool) {
	updated := filterStrings(step.Updated, query)
	skipped := filterStrings(step.SkippedItems, query)
	if len(updated) > 0 || len(skipped) > 0 {
		step.Updated = updated
		step.SkippedItems = skipped
		return step, true
	}
	if stepMatchesQuery(step, query) {
		return step, true
	}
	return Step{}, false
}

func filterStrings(items []string, query string) []string {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return items
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item), needle) {
			out = append(out, item)
		}
	}
	return out
}

func stepMatchesQuery(step Step, query string) bool {
	commands := make([]string, 0, len(step.Commands)+1)
	if len(step.Command) > 0 {
		commands = append(commands, strings.Join(step.Command, " "))
	}
	for _, command := range step.Commands {
		commands = append(commands, strings.Join(command.Command, " "))
	}
	haystack := strings.ToLower(strings.Join([]string{
		step.Name,
		string(step.Status),
		strings.Join(commands, " ; "),
		step.Reason,
		step.Stdout,
		step.Stderr,
		strings.Join(step.Updated, " "),
		strings.Join(step.SkippedItems, " "),
	}, " "))
	return strings.Contains(haystack, strings.ToLower(strings.TrimSpace(query)))
}

func filterGates(gates []securitygate.Gate, filter Filter) []securitygate.Gate {
	if filter.Provider == "" && filter.Status == "" && filter.Query == "" {
		return gates
	}
	out := make([]securitygate.Gate, 0, len(gates))
	for _, gate := range gates {
		if filter.Provider != "" && !strings.EqualFold(gate.Provider, filter.Provider) {
			continue
		}
		filtered := gate
		filtered.Findings = filterFindings(gate.Findings, filter)
		if !gateMatches(gate, filter) && len(filtered.Findings) == 0 {
			continue
		}
		out = append(out, filtered)
	}
	return out
}

func filterFindings(findings []securitygate.Finding, filter Filter) []securitygate.Finding {
	if filter.Status == "" && filter.Query == "" {
		return findings
	}
	out := make([]securitygate.Finding, 0, len(findings))
	for _, finding := range findings {
		if filter.Status != "" && !findingStatusMatches(finding, filter.Status) {
			continue
		}
		if filter.Query != "" && !findingMatchesQuery(finding, filter.Query) {
			continue
		}
		out = append(out, finding)
	}
	return out
}

func gateMatches(gate securitygate.Gate, filter Filter) bool {
	if filter.Status != "" && !plan.StatusMatches(gate.Status, filter.Status) {
		return false
	}
	if filter.Query == "" {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		gate.Provider,
		string(gate.Status),
		gate.Error,
		strings.Join(gate.Warnings, " "),
		primaryGateReason(gate),
	}, " "))
	return strings.Contains(haystack, strings.ToLower(strings.TrimSpace(filter.Query)))
}

func primaryGateReason(gate securitygate.Gate) string {
	if gate.Error != "" {
		return gate.Error
	}
	for _, warning := range gate.Warnings {
		if warning != "" {
			return "warning: " + warning
		}
	}
	for _, finding := range gate.Findings {
		if securitygate.DecisionNeedsAttention(finding.Decision) {
			if reason := strings.TrimSpace(finding.Reason); reason != "" {
				return reason
			}
		}
	}
	return ""
}

func findingStatusMatches(finding securitygate.Finding, filter string) bool {
	normalized := strings.ToLower(strings.TrimSpace(filter))
	switch normalized {
	case "attention", "problem", "problems":
		return securitygate.DecisionNeedsAttention(finding.Decision)
	default:
		return strings.EqualFold(finding.Decision, normalized)
	}
}

func findingMatchesQuery(finding securitygate.Finding, query string) bool {
	haystack := strings.ToLower(strings.Join([]string{
		finding.Provider,
		finding.Kind,
		finding.Name,
		strings.TrimSpace(finding.Kind + "/" + finding.Name),
		finding.Decision,
		finding.Reason,
		finding.Remediation,
		strings.Join(finding.Evidence, " "),
		finding.Source,
		finding.Tap,
		finding.Publisher,
		finding.RepositoryURL,
		finding.SupportURL,
		finding.Homepage,
		finding.URL,
		finding.Confidence,
		strings.Join(finding.AdvisoryIDs, " "),
		strings.Join(finding.FixedVersions, " "),
	}, " "))
	return strings.Contains(haystack, strings.ToLower(strings.TrimSpace(query)))
}
