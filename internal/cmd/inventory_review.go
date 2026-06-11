package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
)

const manualAgentEnrichmentMaxBatch = 20

type inventoryReviewOptions struct {
	action   string
	format   string
	limit    int
	provider string
	query    string
	root     string
}

type inventoryReviewReport struct {
	SchemaVersion   int                     `json:"schema_version"`
	Status          plan.Status             `json:"status"`
	Root            string                  `json:"root"`
	Provider        string                  `json:"provider"`
	Action          string                  `json:"action,omitempty"`
	Query           string                  `json:"query,omitempty"`
	Changed         bool                    `json:"changed,omitempty"`
	ChangedPath     string                  `json:"changed_path,omitempty"`
	OverridesPath   string                  `json:"overrides_path,omitempty"`
	Candidates      []manualReviewCandidate `json:"candidates,omitempty"`
	Applied         []manualReviewCandidate `json:"applied,omitempty"`
	Overrides       []manualAppOverride     `json:"overrides,omitempty"`
	ChangedOverride *manualAppOverride      `json:"changed_override,omitempty"`
	Drafts          []manualStructuredApp   `json:"drafts,omitempty"`
	OverridePreview string                  `json:"override_preview,omitempty"`
}

type manualAppOverrideRawBlock struct {
	Override manualAppOverride
	Start    int
	End      int
}

type manualStructuredAppRawBlock struct {
	App   manualStructuredApp
	Start int
	End   int
	Text  string
}

func parseInventoryReviewOptions(args []string) (inventoryReviewOptions, error) {
	opts := inventoryReviewOptions{format: "text", provider: manualProviderName, root: defaultRoot()}
	fs := flag.NewFlagSet("inventory review", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.action, "action", opts.action, "review action: preview, list, accept, edit, ignore, update, remove, enrich, or enrich-batch")
	fs.StringVar(&opts.format, "format", opts.format, "output format: text or json")
	fs.IntVar(&opts.limit, "limit", opts.limit, "maximum candidates for batch actions; 0 means default")
	fs.StringVar(&opts.provider, "provider", opts.provider, "inventory provider to review")
	fs.StringVar(&opts.query, "query", opts.query, "case-insensitive candidate filter")
	fs.StringVar(&opts.root, "root", opts.root, "chezmoi source root")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if opts.provider == "" {
		return opts, fmt.Errorf("--provider requires a value")
	}
	if !strings.EqualFold(opts.provider, manualProviderName) {
		return opts, fmt.Errorf("unsupported inventory review provider: %s", opts.provider)
	}
	opts.provider = manualProviderName
	opts.action = strings.TrimSpace(strings.ToLower(opts.action))
	if opts.action == "preview" {
		opts.action = ""
	}
	switch opts.action {
	case "", "list", "accept", "edit", "ignore", "update", "remove", "enrich", "enrich-batch":
	default:
		return opts, fmt.Errorf("unsupported inventory review action: %s", opts.action)
	}
	if opts.limit < 0 {
		return opts, fmt.Errorf("--limit must be 0 or greater")
	}
	opts.query = strings.TrimSpace(opts.query)
	return opts, nil
}

func runInventoryReview(opts inventoryReviewOptions) int {
	report := buildInventoryReviewReport(opts)
	if opts.action != "" {
		if opts.action == "list" {
			if opts.format == "json" {
				return encodeJSON(report)
			}
			printInventoryReviewText(report)
			return 0
		}
		applied, changedOverride, drafts, err := applyInventoryReviewAction(opts, report)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		postOpts := opts
		postOpts.query = ""
		report = buildInventoryReviewReport(postOpts)
		report.Action = opts.action
		report.Query = opts.query
		report.Changed = true
		report.ChangedPath = report.OverridesPath
		if opts.action == "enrich" || opts.action == "enrich-batch" {
			report.ChangedPath = manualAgentDraftSourcePath(opts.root)
		}
		if applied.Name != "" {
			report.Applied = []manualReviewCandidate{applied}
		}
		if changedOverride.Name != "" {
			report.ChangedOverride = &changedOverride
		}
		report.Drafts = drafts
	}
	if opts.format == "json" {
		code := encodeJSON(report)
		if code != 0 {
			return code
		}
		return inventoryReviewExitCode(report)
	}
	printInventoryReviewText(report)
	return inventoryReviewExitCode(report)
}

func inventoryReviewExitCode(report inventoryReviewReport) int {
	if report.Status == plan.StatusDrift {
		return 2
	}
	return 0
}

func buildInventoryReviewReport(opts inventoryReviewOptions) inventoryReviewReport {
	sections := manualAppSectionsForInventoryCommand(opts.root)
	candidates := manualReviewCandidates(sections)
	overrides := loadManualAppOverrides(opts.root)
	if inventoryReviewActionUsesOverrides(opts.action) {
		overrides = filterManualOverrides(overrides, opts.query)
		candidates = nil
	} else {
		candidates = filterManualReviewCandidates(candidates, opts.query)
	}
	status := plan.StatusOK
	if len(candidates) > 0 {
		status = plan.StatusDrift
	}
	return inventoryReviewReport{
		SchemaVersion:   1,
		Status:          status,
		Root:            opts.root,
		Provider:        manualProviderName,
		Action:          opts.action,
		Query:           opts.query,
		OverridesPath:   configuredInventoryOverridesPath(opts.root),
		Candidates:      candidates,
		Overrides:       overrides,
		OverridePreview: renderManualOverridePreview(candidates),
	}
}

func inventoryReviewActionUsesOverrides(action string) bool {
	switch action {
	case "list", "update", "remove":
		return true
	default:
		return false
	}
}

func configuredInventoryOverridesPath(root string) string {
	if path := inventoryOverridesPath(root); path != "" {
		return path
	}
	return resolveUpdevConfigPath(root, ".config/updev/inventory-overrides.toml")
}

func printInventoryReviewText(report inventoryReviewReport) {
	fmt.Printf("inventory review: %s\n", report.Status)
	fmt.Printf("provider: %s\n", report.Provider)
	if report.Action != "" {
		fmt.Printf("action: %s\n", report.Action)
	}
	if report.Query != "" {
		fmt.Printf("query: %s\n", report.Query)
	}
	if report.OverridesPath != "" {
		fmt.Printf("overrides: %s\n", report.OverridesPath)
	}
	if report.Changed {
		fmt.Printf("changed: %s\n", report.ChangedPath)
	}
	if len(report.Drafts) > 0 {
		fmt.Printf("drafts: %d\n", len(report.Drafts))
	}
	fmt.Printf("candidates: %d\n", len(report.Candidates))
	if len(report.Overrides) > 0 || report.Action == "list" || report.Action == "update" || report.Action == "remove" {
		printInventoryReviewOverrides(report)
	}
	if len(report.Candidates) == 0 {
		return
	}
	printInventoryReviewActionCommands(report)
	fmt.Println()
	fmt.Println(report.OverridePreview)
}

func printInventoryReviewOverrides(report inventoryReviewReport) {
	fmt.Printf("overrides: %d\n", len(report.Overrides))
	if len(report.Overrides) == 0 {
		return
	}
	fmt.Println()
	fmt.Println("manual overrides")
	for _, override := range report.Overrides {
		fmt.Printf("  %s managed_by=%s lifecycle=%s category=%s\n", override.Name, firstNonEmpty(override.ManagedBy, "-"), firstNonEmpty(override.Lifecycle, "-"), firstNonEmpty(override.Category, "-"))
		if len(override.Aliases) > 0 {
			fmt.Printf("    aliases: %s\n", strings.Join(override.Aliases, ", "))
		}
		if override.Detail != "" {
			fmt.Printf("    detail: %s\n", override.Detail)
		}
	}
	if report.Action == "list" {
		fmt.Println()
		fmt.Println("next:")
		fmt.Println("  updev inventory review --provider manual --action update --query <name>")
		fmt.Println("  updev inventory review --provider manual --action remove --query <name>")
	}
}

func printInventoryReviewActionCommands(report inventoryReviewReport) {
	if len(report.Candidates) != 1 {
		fmt.Println()
		fmt.Println("next:")
		fmt.Println("  refine --query until it matches exactly one candidate before using --action")
		return
	}
	name := report.Candidates[0].Name
	query := name
	if report.Query != "" {
		query = report.Query
	}
	fmt.Println()
	fmt.Println("next:")
	for _, action := range []string{"accept", "edit", "ignore"} {
		fmt.Printf("  updev inventory review --provider manual --action %s --query %s\n", action, strconv.Quote(query))
	}
	fmt.Printf("  updev inventory review --provider manual --action enrich --query %s\n", strconv.Quote(query))
	fmt.Println("  updev inventory review --provider manual --action enrich-batch --query <filter>")
	fmt.Println("  existing override handling: matching duplicates are blocked; use --action list|update|remove for existing overrides")
}

func applyInventoryReviewAction(opts inventoryReviewOptions, report inventoryReviewReport) (manualReviewCandidate, manualAppOverride, []manualStructuredApp, error) {
	if opts.action == "enrich" || opts.action == "enrich-batch" {
		drafts, err := applyManualAgentEnrichmentAction(opts, report.Candidates)
		return manualReviewCandidate{}, manualAppOverride{}, drafts, err
	}
	if opts.action == "remove" || opts.action == "update" {
		changed, err := applyManualOverrideManagementAction(opts, report.Overrides)
		return manualReviewCandidate{}, changed, nil, err
	}
	candidate, err := selectInventoryReviewCandidate(report.Candidates, opts.query)
	if err != nil {
		return manualReviewCandidate{}, manualAppOverride{}, nil, err
	}
	if existing := matchingManualOverride(opts.root, candidate); existing.Name != "" {
		return manualReviewCandidate{}, manualAppOverride{}, nil, fmt.Errorf("manual inventory override already exists for %q; use --action update or remove after inspecting %s", existing.Name, configuredInventoryOverridesPath(opts.root))
	}
	content := manualOverrideBlockForAction(opts.action, candidate)
	if opts.action == "edit" {
		content, err = editManualOverrideBlock(content)
		if err != nil {
			return manualReviewCandidate{}, manualAppOverride{}, nil, err
		}
	}
	if err := appendManualOverrideBlock(configuredInventoryOverridesPath(opts.root), content); err != nil {
		return manualReviewCandidate{}, manualAppOverride{}, nil, err
	}
	return candidate, manualAppOverride{}, nil, nil
}

type manualAgentEnrichmentRequest struct {
	SchemaVersion int                     `json:"schema_version"`
	Provider      string                  `json:"provider"`
	Action        string                  `json:"action"`
	Candidates    []manualReviewCandidate `json:"candidates"`
	Output        string                  `json:"output"`
	Instructions  []string                `json:"instructions"`
}

func applyManualAgentEnrichmentAction(opts inventoryReviewOptions, candidates []manualReviewCandidate) ([]manualStructuredApp, error) {
	config := loadUpdevConfig()
	if config.Inventory.Agent.Enabled == nil || !*config.Inventory.Agent.Enabled {
		return nil, fmt.Errorf("manual inventory agent enrichment is disabled; set [inventory.agent].enabled = true")
	}
	if len(config.Inventory.Agent.Command) == 0 {
		return nil, fmt.Errorf("manual inventory agent command is not configured")
	}
	sourcePath := manualAgentDraftSourcePath(opts.root)
	if sourcePath == "" {
		return nil, fmt.Errorf("manual inventory agent enrichment requires a configured TOML source in [inventory.manual].sources")
	}
	selected, err := selectManualAgentEnrichmentCandidates(opts, candidates, config)
	if err != nil {
		return nil, err
	}
	request := manualAgentEnrichmentRequest{
		SchemaVersion: 1,
		Provider:      manualProviderName,
		Action:        opts.action,
		Candidates:    selected,
		Output:        "TOML [[manual.apps]] entries with review_status = \"draft\"",
		Instructions: []string{
			"Return only TOML. Do not include prose or markdown fences.",
			"Use one [[manual.apps]] entry per candidate.",
			"Keep review_status = \"draft\" for every entry.",
			"Use [manual.apps.identifiers] for bundle_id, mas_id, cask, or path when known.",
			"Use [manual.apps.provenance] source = \"agent\" and evidence = [...].",
			"Add source_url or review_url when a vendor or provider page is known.",
			"Add owner, update_owner, and provider_metadata when ownership evidence is known.",
		},
	}
	payload, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return nil, err
	}
	stdout, err := runManualAgentCommand(config.Inventory.Agent.Command, payload)
	if err != nil {
		return nil, err
	}
	drafts, err := validateManualAgentDrafts(stdout, selected, config.Inventory.Agent.Command)
	if err != nil {
		return nil, err
	}
	if err := appendManualStructuredDrafts(sourcePath, drafts); err != nil {
		return nil, err
	}
	return drafts, nil
}

func selectManualAgentEnrichmentCandidates(opts inventoryReviewOptions, candidates []manualReviewCandidate, config updevConfig) ([]manualReviewCandidate, error) {
	if opts.action == "enrich" {
		candidate, err := selectInventoryReviewCandidate(candidates, opts.query)
		if err != nil {
			return nil, err
		}
		return []manualReviewCandidate{candidate}, nil
	}
	if opts.action != "enrich-batch" {
		return nil, fmt.Errorf("unsupported manual inventory agent action: %s", opts.action)
	}
	if config.Inventory.Agent.Batch == nil || !*config.Inventory.Agent.Batch {
		return nil, fmt.Errorf("manual inventory batch enrichment is disabled; set [inventory.agent].batch = true")
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no manual inventory review candidates")
	}
	limit := opts.limit
	if limit == 0 || limit > manualAgentEnrichmentMaxBatch {
		limit = manualAgentEnrichmentMaxBatch
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func manualAgentDraftSourcePath(root string) string {
	paths := configuredManualStructuredInventoryPaths(root)
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func manualAgentEnrichmentAvailable(root string) bool {
	config := loadUpdevConfig()
	return config.Inventory.Agent.Enabled != nil && *config.Inventory.Agent.Enabled &&
		len(config.Inventory.Agent.Command) > 0 &&
		manualAgentDraftSourcePath(root) != ""
}

func runManualAgentCommand(command []string, payload []byte) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("manual inventory agent command timed out")
		}
		return "", fmt.Errorf("manual inventory agent command failed: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func validateManualAgentDrafts(content string, candidates []manualReviewCandidate, command []string) ([]manualStructuredApp, error) {
	drafts := parseManualStructuredApps(content)
	if len(drafts) == 0 {
		return nil, fmt.Errorf("manual inventory agent output did not contain any [[manual.apps]] draft entries")
	}
	candidateIndex := map[string]manualReviewCandidate{}
	for _, candidate := range candidates {
		for _, key := range manualReviewCandidateIdentityKeys(candidate) {
			if key != "" {
				candidateIndex[key] = candidate
			}
		}
	}
	out := make([]manualStructuredApp, 0, len(drafts))
	seen := map[string]bool{}
	for _, draft := range drafts {
		match, ok := manualAgentDraftMatchesCandidate(draft, candidateIndex)
		if !ok {
			return nil, fmt.Errorf("manual inventory agent draft %q does not match selected candidates", draft.Name)
		}
		draft.ReviewStatus = "draft"
		if draft.Provenance == nil {
			draft.Provenance = map[string]string{}
		}
		draft.Provenance["source"] = "agent"
		if len(command) > 0 && draft.Provenance["command"] == "" {
			draft.Provenance["command"] = filepath.Base(command[0])
		}
		for _, evidence := range match.Evidence {
			if evidence.Scanner != "" && !stringListContains(draft.Evidence, evidence.Scanner) {
				draft.Evidence = append(draft.Evidence, evidence.Scanner)
			}
			if draft.Provenance["source_url"] == "" {
				draft.Provenance["source_url"] = firstNonEmpty(evidence.SourceURL, evidence.ReviewURL)
			}
			if draft.Provenance["owner"] == "" {
				draft.Provenance["owner"] = evidence.Owner
			}
			if draft.Provenance["update_owner"] == "" {
				draft.Provenance["update_owner"] = evidence.UpdateOwner
			}
			if draft.Provenance["provider_metadata"] == "" {
				draft.Provenance["provider_metadata"] = evidence.ProviderMetadata
			}
		}
		key := normalizedManualAppKey(draft.Name)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, draft)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("manual inventory agent output did not contain usable draft entries")
	}
	return out, nil
}

func manualReviewCandidateIdentityKeys(candidate manualReviewCandidate) []string {
	keys := []string{}
	for _, key := range manualAppKeys(candidate.Name) {
		keys = append(keys, "name:"+key)
	}
	for _, alias := range candidate.SuggestedOverride.Aliases {
		if key := normalizedManualAppKey(alias); key != "" {
			keys = append(keys, "name:"+key)
		}
	}
	for _, evidence := range candidate.Evidence {
		if evidence.BundleID != "" {
			keys = append(keys, "bundle:"+strings.ToLower(evidence.BundleID))
		}
		if evidence.MASID != "" {
			keys = append(keys, "mas:"+strings.ToLower(evidence.MASID))
		}
		if evidence.Path != "" {
			for _, key := range manualAppPathKeys(evidence.Path) {
				keys = append(keys, "name:"+key)
			}
		}
	}
	return keys
}

func manualAgentDraftMatchesCandidate(draft manualStructuredApp, candidateIndex map[string]manualReviewCandidate) (manualReviewCandidate, bool) {
	row := manualStructuredAppRow(draft)
	for _, key := range manualRowIdentityKeys(row) {
		if candidate, ok := candidateIndex[key]; ok {
			return candidate, true
		}
	}
	return manualReviewCandidate{}, false
}

func appendManualStructuredDrafts(path string, drafts []manualStructuredApp) error {
	blocks := make([]string, 0, len(drafts))
	for _, draft := range drafts {
		blocks = append(blocks, renderManualStructuredDraftBlock(draft))
	}
	return appendManualOverrideBlock(path, strings.Join(blocks, "\n"))
}

func stringListContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func renderManualStructuredDraftBlock(app manualStructuredApp) string {
	app.ReviewStatus = "draft"
	if app.Provenance == nil {
		app.Provenance = map[string]string{}
	}
	app.Provenance["source"] = "agent"
	return renderManualStructuredAppBlock(app)
}

func renderManualStructuredAppBlock(app manualStructuredApp) string {
	var builder strings.Builder
	builder.WriteString("[[manual.apps]]\n")
	builder.WriteString("name = ")
	builder.WriteString(tomlString(app.Name))
	builder.WriteString("\n")
	if len(app.Aliases) > 0 {
		builder.WriteString("aliases = ")
		builder.WriteString(tomlStringArray(app.Aliases))
		builder.WriteString("\n")
	}
	if app.Category != "" {
		builder.WriteString("category = ")
		builder.WriteString(tomlString(app.Category))
		builder.WriteString("\n")
	}
	if app.ManagedBy != "" {
		builder.WriteString("managed_by = ")
		builder.WriteString(tomlString(app.ManagedBy))
		builder.WriteString("\n")
	}
	if app.Lifecycle != "" {
		builder.WriteString("lifecycle = ")
		builder.WriteString(tomlString(app.Lifecycle))
		builder.WriteString("\n")
	}
	if app.Detail != "" {
		builder.WriteString("description = ")
		builder.WriteString(tomlString(app.Detail))
		builder.WriteString("\n")
	}
	if app.Confidence != "" {
		builder.WriteString("confidence = ")
		builder.WriteString(tomlString(app.Confidence))
		builder.WriteString("\n")
	}
	builder.WriteString("review_status = ")
	builder.WriteString(tomlString(firstNonEmpty(app.ReviewStatus, "draft")))
	builder.WriteString("\n")
	if len(app.Identifiers) > 0 {
		builder.WriteString("\n[manual.apps.identifiers]\n")
		for _, key := range []string{"bundle_id", "mas_id", "cask", "path"} {
			if value := app.Identifiers[key]; value != "" {
				builder.WriteString(key)
				builder.WriteString(" = ")
				builder.WriteString(tomlString(value))
				builder.WriteString("\n")
			}
		}
	}
	if len(app.Provenance) > 0 || len(app.Evidence) > 0 {
		builder.WriteString("\n[manual.apps.provenance]\n")
		if source := firstNonEmpty(app.Provenance["source"], "agent"); source != "" {
			builder.WriteString("source = ")
			builder.WriteString(tomlString(source))
			builder.WriteString("\n")
		}
		if command := app.Provenance["command"]; command != "" {
			builder.WriteString("command = ")
			builder.WriteString(tomlString(command))
			builder.WriteString("\n")
		}
		for _, key := range manualStructuredProvenanceDetailKeys() {
			if value := app.Provenance[key]; value != "" {
				builder.WriteString(key)
				builder.WriteString(" = ")
				builder.WriteString(tomlString(value))
				builder.WriteString("\n")
			}
		}
		if reviewedAt := app.Provenance["reviewed_at"]; reviewedAt != "" {
			builder.WriteString("reviewed_at = ")
			builder.WriteString(tomlString(reviewedAt))
			builder.WriteString("\n")
		}
		if len(app.Evidence) > 0 {
			builder.WriteString("evidence = ")
			builder.WriteString(tomlStringArray(app.Evidence))
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func applyManualStructuredDraftAction(root string, action string, query string) error {
	path := manualAgentDraftSourcePath(root)
	if path == "" {
		return fmt.Errorf("manual structured source is not configured")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)
	block, err := selectManualStructuredDraftBlock(parseManualStructuredAppRawBlocks(content), query)
	if err != nil {
		return err
	}
	switch action {
	case "accept-draft":
		app := block.App
		app.ReviewStatus = "accepted"
		return os.WriteFile(path, []byte(replaceManualStructuredBlock(content, block, renderManualStructuredAppBlock(app))), 0o600)
	case "edit-draft":
		edited, err := editManualStructuredAppBlock(renderManualStructuredAppBlock(block.App))
		if err != nil {
			return err
		}
		return os.WriteFile(path, []byte(replaceManualStructuredBlock(content, block, edited)), 0o600)
	case "ignore-draft":
		return os.WriteFile(path, []byte(replaceManualStructuredBlock(content, block, "")), 0o600)
	default:
		return fmt.Errorf("unsupported manual draft action: %s", action)
	}
}

func parseManualStructuredAppRawBlocks(content string) []manualStructuredAppRawBlock {
	type rawBlock struct {
		start int
		end   int
	}
	blocks := []rawBlock{}
	offset := 0
	currentStart := -1
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := stripTOMLComment(strings.TrimSpace(line))
		if strings.HasPrefix(trimmed, "[[") && strings.HasSuffix(trimmed, "]]") {
			section := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "[["), "]]")))
			if section == "manual.apps" {
				if currentStart >= 0 {
					blocks = append(blocks, rawBlock{start: currentStart, end: offset})
				}
				currentStart = offset
			}
		}
		offset += len(line)
	}
	if currentStart >= 0 {
		blocks = append(blocks, rawBlock{start: currentStart, end: len(content)})
	}
	out := make([]manualStructuredAppRawBlock, 0, len(blocks))
	for _, block := range blocks {
		text := content[block.start:block.end]
		apps := parseManualStructuredApps(text)
		if len(apps) != 1 || strings.TrimSpace(apps[0].Name) == "" {
			continue
		}
		out = append(out, manualStructuredAppRawBlock{App: apps[0], Start: block.start, End: block.end, Text: text})
	}
	return out
}

func selectManualStructuredDraftBlock(blocks []manualStructuredAppRawBlock, query string) (manualStructuredAppRawBlock, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	matches := []manualStructuredAppRawBlock{}
	for _, block := range blocks {
		if manualStructuredAppAccepted(block.App) {
			continue
		}
		if query == "" || manualStructuredAppMatchesQuery(block.App, query) {
			matches = append(matches, block)
		}
	}
	switch len(matches) {
	case 0:
		if query == "" {
			return manualStructuredAppRawBlock{}, fmt.Errorf("no manual structured draft entries")
		}
		return manualStructuredAppRawBlock{}, fmt.Errorf("no manual structured draft entries match %q", query)
	case 1:
		return matches[0], nil
	default:
		return manualStructuredAppRawBlock{}, fmt.Errorf("manual draft action requires a query matching exactly one draft; matched %d", len(matches))
	}
}

func manualStructuredAppMatchesQuery(app manualStructuredApp, query string) bool {
	parts := []string{app.Name, app.Category, app.Detail, app.ManagedBy, app.Lifecycle, app.Confidence, app.ReviewStatus}
	parts = append(parts, app.Aliases...)
	for _, key := range []string{"bundle_id", "mas_id", "cask", "path"} {
		parts = append(parts, app.Identifiers[key])
	}
	for _, value := range app.Provenance {
		parts = append(parts, value)
	}
	parts = append(parts, app.Evidence...)
	return strings.Contains(strings.ToLower(strings.Join(parts, " ")), query)
}

func replaceManualStructuredBlock(content string, block manualStructuredAppRawBlock, replacement string) string {
	replacement = strings.TrimSpace(replacement)
	prefix := content[:block.Start]
	suffix := content[block.End:]
	if replacement != "" {
		replacement += "\n"
		if strings.TrimSpace(prefix) != "" && !strings.HasSuffix(prefix, "\n\n") {
			replacement = "\n" + replacement
		}
	}
	return strings.TrimRight(prefix+replacement+strings.TrimLeft(suffix, "\n"), "\n") + "\n"
}

func editManualStructuredAppBlock(content string) (string, error) {
	edited, err := editManualOverrideBlock(content)
	if err != nil {
		return "", err
	}
	apps := parseManualStructuredApps(edited)
	if len(apps) != 1 || strings.TrimSpace(apps[0].Name) == "" {
		return "", fmt.Errorf("edited draft must contain exactly one [[manual.apps]] entry with name")
	}
	apps[0].ReviewStatus = "draft"
	return renderManualStructuredAppBlock(apps[0]), nil
}

func applyManualOverrideManagementAction(opts inventoryReviewOptions, overrides []manualAppOverride) (manualAppOverride, error) {
	selected, err := selectManualOverride(overrides, opts.query)
	if err != nil {
		return manualAppOverride{}, err
	}
	path := configuredInventoryOverridesPath(opts.root)
	data, err := os.ReadFile(path)
	if err != nil {
		return manualAppOverride{}, err
	}
	content := string(data)
	block, ok := selectManualOverrideRawBlock(content, selected)
	if !ok {
		return manualAppOverride{}, fmt.Errorf("selected manual override disappeared before write")
	}
	changed := selected
	replacement := ""
	switch opts.action {
	case "remove":
	case "update":
		replacement = renderManualAppOverrideBlock(selected)
		replacement, err = editManualOverrideBlock(replacement)
		if err != nil {
			return manualAppOverride{}, err
		}
		updated := parseManualAppOverrides(replacement)
		if len(updated) != 1 {
			return manualAppOverride{}, fmt.Errorf("updated override must contain exactly one [[manual.apps]] entry")
		}
		changed = updated[0]
	default:
		return manualAppOverride{}, fmt.Errorf("unsupported override management action: %s", opts.action)
	}
	if err := os.WriteFile(path, []byte(replaceManualOverrideRawBlock(content, block, replacement)), 0o600); err != nil {
		return manualAppOverride{}, err
	}
	return changed, nil
}

func matchingManualOverride(root string, candidate manualReviewCandidate) manualAppOverride {
	candidateKeys := map[string]bool{}
	for _, name := range append([]string{candidate.Name, candidate.SuggestedOverride.Name}, candidate.SuggestedOverride.Aliases...) {
		for _, key := range manualAppKeys(name) {
			if key != "" {
				candidateKeys[key] = true
			}
		}
	}
	for _, override := range loadManualAppOverrides(root) {
		for _, name := range append([]string{override.Name}, override.Aliases...) {
			for _, key := range manualAppKeys(name) {
				if candidateKeys[key] {
					return override
				}
			}
		}
	}
	return manualAppOverride{}
}

func filterManualOverrides(overrides []manualAppOverride, query string) []manualAppOverride {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return overrides
	}
	out := []manualAppOverride{}
	for _, override := range overrides {
		if manualOverrideMatches(override, query) {
			out = append(out, override)
		}
	}
	return out
}

func manualOverrideMatches(override manualAppOverride, query string) bool {
	parts := []string{override.Name, override.Category, override.Detail, override.ManagedBy, override.Lifecycle}
	parts = append(parts, override.Aliases...)
	return strings.Contains(strings.ToLower(strings.Join(parts, " ")), query)
}

func selectManualOverride(overrides []manualAppOverride, query string) (manualAppOverride, error) {
	switch len(overrides) {
	case 0:
		if query == "" {
			return manualAppOverride{}, fmt.Errorf("no manual inventory overrides")
		}
		return manualAppOverride{}, fmt.Errorf("no manual inventory overrides match %q", query)
	case 1:
		return overrides[0], nil
	default:
		return manualAppOverride{}, fmt.Errorf("--action requires --query to match exactly one override; matched %d", len(overrides))
	}
}

func manualOverridesSameIdentity(left manualAppOverride, right manualAppOverride) bool {
	leftKeys := map[string]bool{}
	for _, name := range append([]string{left.Name}, left.Aliases...) {
		for _, key := range manualAppKeys(name) {
			if key != "" {
				leftKeys[key] = true
			}
		}
	}
	for _, name := range append([]string{right.Name}, right.Aliases...) {
		for _, key := range manualAppKeys(name) {
			if leftKeys[key] {
				return true
			}
		}
	}
	return false
}

func selectManualOverrideRawBlock(content string, selected manualAppOverride) (manualAppOverrideRawBlock, bool) {
	for _, block := range manualAppOverrideRawBlocks(content) {
		if manualOverridesSameIdentity(block.Override, selected) {
			return block, true
		}
	}
	return manualAppOverrideRawBlock{}, false
}

func manualAppOverrideRawBlocks(content string) []manualAppOverrideRawBlock {
	lines := strings.SplitAfter(content, "\n")
	blocks := []manualAppOverrideRawBlock{}
	currentStart := -1
	offset := 0
	closeBlock := func(end int) {
		if currentStart < 0 {
			return
		}
		end = manualAppOverrideRawBlockEnd(content, currentStart, end)
		blockContent := content[currentStart:end]
		parsed := parseManualAppOverrides(blockContent)
		if len(parsed) == 1 {
			blocks = append(blocks, manualAppOverrideRawBlock{Override: parsed[0], Start: currentStart, End: end})
		}
		currentStart = -1
	}
	for _, line := range lines {
		trimmed := stripTOMLComment(strings.TrimSpace(line))
		if strings.HasPrefix(trimmed, "[[") && strings.HasSuffix(trimmed, "]]") {
			closeBlock(offset)
			section := strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "[["), "]]")))
			if section == "manual.apps" {
				currentStart = offset
			}
		}
		offset += len(line)
	}
	closeBlock(len(content))
	return blocks
}

func manualAppOverrideRawBlockEnd(content string, start int, end int) int {
	lines := strings.SplitAfter(content[start:end], "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			end -= len(line)
			continue
		}
		break
	}
	return end
}

func replaceManualOverrideRawBlock(content string, block manualAppOverrideRawBlock, replacement string) string {
	return content[:block.Start] + replacement + content[block.End:]
}

func selectInventoryReviewCandidate(candidates []manualReviewCandidate, query string) (manualReviewCandidate, error) {
	switch len(candidates) {
	case 0:
		if query == "" {
			return manualReviewCandidate{}, fmt.Errorf("no manual inventory review candidates")
		}
		return manualReviewCandidate{}, fmt.Errorf("no manual inventory review candidates match %q", query)
	case 1:
		return candidates[0], nil
	default:
		return manualReviewCandidate{}, fmt.Errorf("--action requires --query to match exactly one candidate; matched %d", len(candidates))
	}
}

func filterManualReviewCandidates(candidates []manualReviewCandidate, query string) []manualReviewCandidate {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return candidates
	}
	out := make([]manualReviewCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if manualReviewCandidateMatches(candidate, query) {
			out = append(out, candidate)
		}
	}
	return out
}

func manualReviewCandidateMatches(candidate manualReviewCandidate, query string) bool {
	parts := []string{
		candidate.Name,
		candidate.ReasonCode,
		candidate.RemediationCode,
		candidate.Confidence,
		candidate.SuggestedOverride.Name,
		candidate.SuggestedOverride.ManagedBy,
		candidate.SuggestedOverride.Detail,
	}
	parts = append(parts, candidate.SuggestedOverride.Aliases...)
	for _, evidence := range candidate.Evidence {
		parts = append(parts,
			evidence.Scanner,
			evidence.Source,
			evidence.Path,
			evidence.ReviewURL,
			evidence.SourceURL,
			evidence.Owner,
			evidence.ManagedBy,
			evidence.UpdateOwner,
			evidence.OwnershipConfidence,
			evidence.ProviderMetadata,
			evidence.MASID,
			evidence.BundleID,
			evidence.Version,
		)
	}
	return strings.Contains(strings.ToLower(strings.Join(parts, " ")), query)
}

func manualOverrideBlockForAction(action string, candidate manualReviewCandidate) string {
	override := candidate.SuggestedOverride
	if action == "ignore" {
		override.ManagedBy = ""
		override.Category = "Ignored"
		override.Lifecycle = "local-only"
		override.Detail = "local-only app ignored by manual inventory review"
	}
	return renderManualOverrideBlock(candidate, override)
}

func editManualOverrideBlock(content string) (string, error) {
	file, err := os.CreateTemp("", "updev-manual-override-*.toml")
	if err != nil {
		return "", err
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	editorArgs := strings.Fields(editor)
	if len(editorArgs) == 0 {
		editorArgs = []string{"vi"}
	}
	var stderr bytes.Buffer
	command := exec.Command(editorArgs[0], append(editorArgs[1:], path)...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("editor failed: %v %s", err, stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	edited := strings.TrimSpace(string(data))
	if len(parseManualAppOverrides(edited)) == 0 {
		return "", fmt.Errorf("edited override must contain at least one [[manual.apps]] entry with name")
	}
	return edited + "\n", nil
}

func appendManualOverrideBlock(path string, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("manual override content is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var builder strings.Builder
	if len(existing) > 0 {
		builder.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			builder.WriteString("\n")
		}
		builder.WriteString("\n")
	}
	builder.WriteString(strings.TrimRight(content, "\n"))
	builder.WriteString("\n")
	return os.WriteFile(path, []byte(builder.String()), 0o600)
}

func renderManualOverridePreview(candidates []manualReviewCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString("# Generated preview by `updev inventory review --provider manual`.\n")
	builder.WriteString("# Review each entry before copying it into the configured inventory overrides TOML.\n")
	for _, candidate := range candidates {
		builder.WriteString("\n")
		builder.WriteString(strings.TrimRight(renderManualOverrideBlock(candidate, candidate.SuggestedOverride), "\n"))
		builder.WriteString("\n")
	}
	return strings.TrimRight(builder.String(), "\n") + "\n"
}

func renderManualOverrideBlock(candidate manualReviewCandidate, override manualReviewOverrideFields) string {
	var builder strings.Builder
	builder.WriteString("[[manual.apps]]\n")
	builder.WriteString("name = ")
	builder.WriteString(tomlString(override.Name))
	builder.WriteString("\n")
	if len(override.Aliases) > 0 {
		builder.WriteString("aliases = ")
		builder.WriteString(tomlStringArray(override.Aliases))
		builder.WriteString("\n")
	}
	if override.Category != "" {
		builder.WriteString("category = ")
		builder.WriteString(tomlString(override.Category))
		builder.WriteString("\n")
	}
	if override.ManagedBy != "" {
		builder.WriteString("managed_by = ")
		builder.WriteString(tomlString(override.ManagedBy))
		builder.WriteString("\n")
	}
	if override.Lifecycle != "" {
		builder.WriteString("lifecycle = ")
		builder.WriteString(tomlString(override.Lifecycle))
		builder.WriteString("\n")
	}
	if override.Detail != "" {
		builder.WriteString("detail = ")
		builder.WriteString(tomlString(override.Detail))
		builder.WriteString("\n")
	}
	if candidate.ReasonCode != "" {
		builder.WriteString("# reason_code = ")
		builder.WriteString(tomlString(candidate.ReasonCode))
		builder.WriteString("\n")
	}
	if candidate.RemediationCode != "" {
		builder.WriteString("# remediation_code = ")
		builder.WriteString(tomlString(candidate.RemediationCode))
		builder.WriteString("\n")
	}
	if candidate.Confidence != "" {
		builder.WriteString("# confidence = ")
		builder.WriteString(tomlString(candidate.Confidence))
		builder.WriteString("\n")
	}
	for _, evidence := range candidate.Evidence {
		builder.WriteString("# evidence")
		if evidence.Scanner != "" {
			builder.WriteString(" scanner=")
			builder.WriteString(tomlString(evidence.Scanner))
		}
		if evidence.Path != "" {
			builder.WriteString(" path=")
			builder.WriteString(tomlString(evidence.Path))
		}
		if evidence.ReviewURL != "" {
			builder.WriteString(" review_url=")
			builder.WriteString(tomlString(evidence.ReviewURL))
		}
		if evidence.SourceURL != "" {
			builder.WriteString(" source_url=")
			builder.WriteString(tomlString(evidence.SourceURL))
		}
		if evidence.Owner != "" {
			builder.WriteString(" owner=")
			builder.WriteString(tomlString(evidence.Owner))
		}
		if evidence.ManagedBy != "" {
			builder.WriteString(" managed_by=")
			builder.WriteString(tomlString(evidence.ManagedBy))
		}
		if evidence.UpdateOwner != "" {
			builder.WriteString(" update_owner=")
			builder.WriteString(tomlString(evidence.UpdateOwner))
		}
		if evidence.OwnershipConfidence != "" {
			builder.WriteString(" ownership_confidence=")
			builder.WriteString(tomlString(evidence.OwnershipConfidence))
		}
		if evidence.ProviderMetadata != "" {
			builder.WriteString(" provider_metadata=")
			builder.WriteString(tomlString(evidence.ProviderMetadata))
		}
		if evidence.BundleID != "" {
			builder.WriteString(" bundle_id=")
			builder.WriteString(tomlString(evidence.BundleID))
		}
		if evidence.Version != "" {
			builder.WriteString(" version=")
			builder.WriteString(tomlString(evidence.Version))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func renderManualAppOverrideBlock(override manualAppOverride) string {
	fields := manualReviewOverrideFields{
		Name:      override.Name,
		Aliases:   override.Aliases,
		Category:  override.Category,
		Detail:    override.Detail,
		ManagedBy: override.ManagedBy,
		Lifecycle: override.Lifecycle,
	}
	return renderManualOverrideFields(fields)
}

func renderManualOverrideFields(override manualReviewOverrideFields) string {
	var builder strings.Builder
	builder.WriteString("[[manual.apps]]\n")
	builder.WriteString("name = ")
	builder.WriteString(tomlString(override.Name))
	builder.WriteString("\n")
	if len(override.Aliases) > 0 {
		builder.WriteString("aliases = ")
		builder.WriteString(tomlStringArray(override.Aliases))
		builder.WriteString("\n")
	}
	if override.Category != "" {
		builder.WriteString("category = ")
		builder.WriteString(tomlString(override.Category))
		builder.WriteString("\n")
	}
	if override.ManagedBy != "" {
		builder.WriteString("managed_by = ")
		builder.WriteString(tomlString(override.ManagedBy))
		builder.WriteString("\n")
	}
	if override.Lifecycle != "" {
		builder.WriteString("lifecycle = ")
		builder.WriteString(tomlString(override.Lifecycle))
		builder.WriteString("\n")
	}
	if override.Detail != "" {
		builder.WriteString("detail = ")
		builder.WriteString(tomlString(override.Detail))
		builder.WriteString("\n")
	}
	return builder.String()
}

func tomlStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		quoted = append(quoted, tomlString(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func tomlString(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "\n", "\\n")
	value = strings.ReplaceAll(value, "\r", "\\r")
	value = strings.ReplaceAll(value, "\t", "\\t")
	return "\"" + value + "\""
}
