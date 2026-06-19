package cmd

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/legacycache"
	"github.com/webkaz-labs/updev/internal/nativeaudit"
	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/registryaudit"
	"github.com/webkaz-labs/updev/internal/runner"
	"github.com/webkaz-labs/updev/internal/securityadvisory"
	"github.com/webkaz-labs/updev/internal/securitygate"
	"github.com/webkaz-labs/updev/internal/securitypolicy"
	"github.com/webkaz-labs/updev/internal/textui"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

const (
	defaultOSVAPIURL      = "https://api.osv.dev/v1/querybatch"
	defaultCISAKEVURL     = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
	defaultEPSSURL        = "https://api.first.org/data/v1/epss"
	defaultNPMRegistryURL = "https://registry.npmjs.org"
	defaultCratesIOAPIURL = "https://crates.io"
	defaultPyPIAPIURL     = "https://pypi.org/pypi"
)

type (
	npmPosture   = registryaudit.NPMPosture
	cargoPosture = registryaudit.CargoPosture
	pypiPosture  = registryaudit.PyPIPosture
)

type securityOptions struct {
	format        string
	root          string
	provider      string
	ecosystem     string
	policy        string
	scanner       string
	refresh       bool
	includeVSCode bool
}

type securityReviewOptions struct {
	securityOptions
	decision string
	kind     string
	name     string
}

type securityGateOptions struct {
	format        string
	root          string
	provider      string
	policy        string
	includeVSCode bool
}

type securityPolicyOptions struct {
	action  string
	apply   bool
	format  string
	index   int
	path    string
	rule    securityPolicyRule
	set     map[string]bool
	state   string
	ttlDays int
}

type securityReport struct {
	Status   plan.Status        `json:"status"`
	Root     string             `json:"root"`
	Source   string             `json:"source"`
	Sources  []string           `json:"sources,omitempty"`
	Policy   *securityPolicyUse `json:"policy,omitempty"`
	Scanned  []securityPackage  `json:"scanned"`
	Posture  []githubPosture    `json:"posture,omitempty"`
	Brew     []homebrewPosture  `json:"homebrew,omitempty"`
	VSCode   []vscodePosture    `json:"vscode,omitempty"`
	NPM      []npmPosture       `json:"npm,omitempty"`
	Cargo    []cargoPosture     `json:"cargo,omitempty"`
	PyPI     []pypiPosture      `json:"pypi,omitempty"`
	Audits   []nativeAudit      `json:"audits,omitempty"`
	Scanners []scannerEvidence  `json:"scanners,omitempty"`
	Skipped  []securitySkipped  `json:"skipped,omitempty"`
	Findings []securityFinding  `json:"findings,omitempty"`
	Warnings []string           `json:"warnings,omitempty"`
	Error    string             `json:"error,omitempty"`
}

type securityReviewReport struct {
	Status     plan.Status               `json:"status"`
	Root       string                    `json:"root"`
	Source     string                    `json:"source"`
	Policy     *securityPolicyUse        `json:"policy,omitempty"`
	Filters    *securityReviewFilters    `json:"filters,omitempty"`
	Summary    *securityReviewSummary    `json:"summary,omitempty"`
	Candidates []securityReviewCandidate `json:"candidates,omitempty"`
	Warnings   []string                  `json:"warnings,omitempty"`
	Error      string                    `json:"error,omitempty"`
}

type securityReviewFilters struct {
	Decision string `json:"decision,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Name     string `json:"name,omitempty"`
}

type securityReviewSummary struct {
	Candidates int            `json:"candidates"`
	Decisions  map[string]int `json:"decisions,omitempty"`
	Providers  map[string]int `json:"providers,omitempty"`
}

func npmRegistryURL() string {
	return configuredEnvString(defaultNPMRegistryURL, "UPDEV_NPM_REGISTRY_URL")
}

func cratesIOAPIURL() string {
	return configuredEnvString(defaultCratesIOAPIURL, "UPDEV_CRATES_IO_API_URL")
}

func pypiAPIURL() string {
	return configuredEnvString(defaultPyPIAPIURL, "UPDEV_PYPI_API_URL")
}

type securityReviewCandidate struct {
	Provider       string   `json:"provider"`
	Kind           string   `json:"kind,omitempty"`
	Name           string   `json:"name"`
	Version        string   `json:"version,omitempty"`
	Ecosystem      string   `json:"ecosystem,omitempty"`
	Package        string   `json:"package,omitempty"`
	DependencyKind string   `json:"dependency_kind,omitempty"`
	Decision       string   `json:"decision"`
	Reason         string   `json:"reason,omitempty"`
	Remediation    string   `json:"remediation,omitempty"`
	Evidence       []string `json:"evidence,omitempty"`
	Source         string   `json:"source,omitempty"`
	URL            string   `json:"url,omitempty"`
	Prompt         string   `json:"prompt"`
	PolicyCommand  string   `json:"policy_command,omitempty"`
}

type securityPolicyUse struct {
	Path            string `json:"path,omitempty"`
	Loaded          bool   `json:"loaded"`
	RuleCount       int    `json:"rule_count,omitempty"`
	ActiveRules     int    `json:"active_rules,omitempty"`
	ExpiredRules    int    `json:"expired_rules,omitempty"`
	InvalidRules    int    `json:"invalid_rules,omitempty"`
	DuplicateRules  int    `json:"duplicate_rules,omitempty"`
	ShadowedRules   int    `json:"shadowed_rules,omitempty"`
	MissingReasons  int    `json:"missing_reasons,omitempty"`
	MissingExpiries int    `json:"missing_expiries,omitempty"`
	BroadRules      int    `json:"broad_rules,omitempty"`
	Error           string `json:"error,omitempty"`
}

type securityGateReport struct {
	Status   plan.Status        `json:"status"`
	Root     string             `json:"root"`
	Provider string             `json:"provider,omitempty"`
	Policy   *securityPolicyUse `json:"policy,omitempty"`
	Gates    []safetyGate       `json:"gates,omitempty"`
	Warnings []string           `json:"warnings,omitempty"`
	Error    string             `json:"error,omitempty"`
}

type securityPolicyReport struct {
	Status  plan.Status              `json:"status"`
	Path    string                   `json:"path,omitempty"`
	Summary *securityPolicySummary   `json:"summary,omitempty"`
	Rules   []securityPolicyRuleView `json:"rules,omitempty"`
	Cleanup []securityPolicyCleanup  `json:"cleanup,omitempty"`
	Error   string                   `json:"error,omitempty"`
}

type securityPolicyCleanup struct {
	Action   string `json:"action"`
	Index    int    `json:"index"`
	Provider string `json:"provider,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Name     string `json:"name"`
	Decision string `json:"decision"`
	State    string `json:"state"`
	Reason   string `json:"reason"`
	Command  string `json:"command,omitempty"`
	Applied  bool   `json:"applied,omitempty"`
}

type securityPolicySummary = securitypolicy.Summary

type securityPolicyRuleView = securitypolicy.RuleView

type (
	securityPackage  = securityadvisory.Package
	homebrewPosture  = brew.Posture
	homebrewMetadata = brew.Metadata
	homebrewVersions = brew.MetadataVersions
	homebrewURLs     = brew.MetadataURLs
	homebrewURL      = brew.MetadataURL
)

const defaultHomebrewAPIURL = "https://formulae.brew.sh/api"

func homebrewPosturesFromItems(ctx context.Context, client *http.Client, apiBase string, root string, items []plan.Item) ([]homebrewPosture, error) {
	manifest, err := loadBrewSafetyManifest(root)
	if err != nil {
		manifest = brewSafetyManifest{}
	}
	return brew.PosturesFromItems(ctx, client, apiBase, items, func(kind string, name string) brew.ManifestEntry {
		entry := manifest.entry(kind, name)
		return homebrewAuditManifestEntry(entry)
	})
}

func homebrewTapPosture(item plan.Item) homebrewPosture {
	return brew.TapPosture(item)
}

func fetchHomebrewMetadata(ctx context.Context, client *http.Client, apiBase string, kind string, name string) (homebrewMetadata, error) {
	return brew.FetchMetadata(ctx, client, apiBase, kind, name)
}

func homebrewPostureFromMetadata(item plan.Item, entry brewSafetyEntry, metadata homebrewMetadata) homebrewPosture {
	return brew.PostureFromMetadata(item, homebrewAuditManifestEntry(entry), metadata)
}

func homebrewManifestPosture(item plan.Item, entry brewSafetyEntry, reason string) homebrewPosture {
	return brew.ManifestPosture(item, homebrewAuditManifestEntry(entry), reason)
}

func homebrewMetadataUnavailable(item plan.Item, entry brewSafetyEntry, err error) homebrewPosture {
	return brew.MetadataUnavailable(item, homebrewAuditManifestEntry(entry), err)
}

func homebrewAuditManifestEntry(entry brewSafetyEntry) brew.ManifestEntry {
	return brew.ManifestEntry{
		RawName:  entry.RawName,
		Tap:      entry.Tap,
		URLBased: entry.URLBased,
	}
}

func homebrewAdvisoryPackagesFromPostures(postures []homebrewPosture) []securityPackage {
	return brew.AdvisoryPackagesFromPostures(postures)
}

func homebrewAdvisoryPackages(kind string, name string, version string, rawURL string) []securityPackage {
	return brew.AdvisoryPackages(kind, name, version, rawURL)
}

func homebrewAPIURL() string {
	return configuredEnvString(defaultHomebrewAPIURL, "UPDEV_HOMEBREW_API_URL")
}

func hasHomebrewPostureReview(postures []homebrewPosture) bool {
	return brew.HasPostureReview(postures)
}

func homebrewPostureReviewCount(postures []homebrewPosture) int {
	return brew.PostureReviewCount(postures)
}

type (
	homebrewTrustTarget = brew.TrustTarget
	homebrewTrustState  = brew.TrustState
)

func homebrewTapTrustDependencyCheck(ctx context.Context, commandRunner runner.Runner, root string) dependencyContractCheck {
	check := dependencyContractCheck{
		Tool:     "brew",
		Feature:  "tap-trust",
		Required: false,
		Command:  brew.TrustJSONCommand(),
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
	result := runDependencyCommand(ctx, commandRunner, check.Command[0], check.Command[1:]...)
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
	return updevpath.RootBrewfileTemplate(root)
}

func parseHomebrewTrustTargets(reader io.Reader, source string) ([]homebrewTrustTarget, error) {
	return brew.ParseTrustTargets(reader, source)
}

func parseHomebrewTrustState(raw string) (homebrewTrustState, error) {
	return brew.ParseTrustState(raw)
}

func applyHomebrewTrustState(targets []homebrewTrustTarget, state homebrewTrustState) []homebrewTrustTarget {
	return brew.ApplyTrustState(targets, state)
}

func homebrewTrustTargetCounts(targets []homebrewTrustTarget) (int, int) {
	return brew.TrustTargetCounts(targets)
}

func homebrewUntrustedTargetNames(targets []homebrewTrustTarget, limit int) []string {
	return brew.UntrustedTargetNames(targets, limit)
}

func firstHomebrewUntrustedTrustCommand(targets []homebrewTrustTarget) string {
	return brew.FirstUntrustedTrustCommand(targets)
}

func isHomebrewTrustSecurityAction(action string) bool {
	switch action {
	case securityActionBrewTrustFormula, securityActionBrewTrustCask, securityActionBrewTrustTap:
		return true
	default:
		return false
	}
}

func homebrewTrustDetailAction(gate safetyGate, finding safetyFinding) (detailBrowserAction, bool) {
	provider := firstNonEmpty(finding.Provider, gate.Provider)
	if provider != "brew" || strings.EqualFold(strings.TrimSpace(finding.Decision), "allow") {
		return detailBrowserAction{}, false
	}
	action, kind, target, command, ok := homebrewTrustActionParts(finding)
	if !ok {
		return detailBrowserAction{}, false
	}
	label := tr("trust package", "package を trust")
	description := fmt.Sprintf(tr("run item-scoped %s after reviewing the package provenance", "package の出所確認後、対象 item だけ %s を実行します"), command)
	if action == securityActionBrewTrustTap {
		label = tr("trust whole tap", "tap 全体を trust")
		description = fmt.Sprintf(tr("run %s only if the whole tap and future entries are accepted", "tap 全体と今後の entry も受け入れる場合だけ %s を実行します"), command)
	}
	return detailBrowserAction{
		Value:       securityDetailActionValue(action, "brew", kind, target),
		Label:       label,
		Description: description,
	}, true
}

func homebrewTrustActionParts(finding safetyFinding) (string, string, string, string, bool) {
	kind := strings.ToLower(strings.TrimSpace(firstNonEmpty(finding.Kind, "item")))
	target := firstNonEmpty(finding.TrustTarget, finding.Name)
	if !validHomebrewTrustTarget(target) {
		return "", "", "", "", false
	}
	action := ""
	trustKind := ""
	switch kind {
	case "brew", "formula":
		if strings.TrimSpace(finding.Tap) == "" || isOfficialBrewTap(finding.Tap) {
			return "", "", "", "", false
		}
		action = securityActionBrewTrustFormula
		trustKind = "formula"
	case "cask":
		if strings.TrimSpace(finding.Tap) == "" || isOfficialBrewTap(finding.Tap) {
			return "", "", "", "", false
		}
		action = securityActionBrewTrustCask
		trustKind = "cask"
	case "tap":
		if isOfficialBrewTap(target) {
			return "", "", "", "", false
		}
		action = securityActionBrewTrustTap
		trustKind = "tap"
	default:
		return "", "", "", "", false
	}
	command := ""
	if args, ok := homebrewTrustCommandForSecurityAction(action, target); ok {
		command = joinCommand(args)
	}
	if command == "" {
		command = joinCommand(finding.TrustCommandArgv)
	}
	if command == "" {
		command = strings.TrimSpace(finding.TrustCommand)
	}
	if command == "" {
		return "", "", "", "", false
	}
	return action, trustKind, target, command, true
}

func validHomebrewTrustTarget(target string) bool {
	return brew.ValidTrustTarget(target)
}

func homebrewTrustCommandForSecurityAction(action string, target string) ([]string, bool) {
	if !validHomebrewTrustTarget(target) {
		return nil, false
	}
	trustKind := ""
	switch action {
	case securityActionBrewTrustFormula:
		trustKind = "formula"
	case securityActionBrewTrustCask:
		trustKind = "cask"
	case securityActionBrewTrustTap:
		trustKind = "tap"
	default:
		return nil, false
	}
	command := brew.TrustCommandArgv(trustKind, target)
	return command, len(command) > 0
}

func confirmHomebrewTrustAction(action string, kind string, target string) bool {
	command, ok := homebrewTrustCommandForSecurityAction(action, target)
	if !ok {
		fmt.Fprintf(os.Stderr, "unsupported Homebrew trust target: %s/%s\n", kind, target)
		return false
	}
	description := tr("Trust only this Homebrew package after reviewing its source.", "出所を確認したうえで、この Homebrew package だけを trust します。")
	if action == securityActionBrewTrustTap {
		description = tr("Trust the whole tap only when you accept all current and future entries.", "現在と今後の entry すべてを受け入れる場合だけ、tap 全体を trust します。")
	}
	selected, err := runUpdevSelect("homebrew trust action", fmt.Sprintf(tr("Run %s?", "%s を実行しますか?"), joinCommand(command)), []updevChoice{
		{Value: "apply", Label: tr("Apply", "適用"), Description: description, Selected: true},
		{Value: updevActionBack, Label: tr("Back", "戻る"), Description: tr("Return without writing.", "書き込まずに戻ります。")},
	}, "apply")
	return err == nil && selected == "apply"
}

func applyConfirmedHomebrewTrustDetailAction(report *updateReport, action string, kind string, target string, printResult bool) bool {
	command, ok := homebrewTrustCommandForSecurityAction(action, target)
	if !ok {
		fmt.Fprintf(os.Stderr, "unsupported Homebrew trust target: %s/%s\n", kind, target)
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	local := runner.Local{}
	var result runner.Result
	if printResult {
		fmt.Printf("%s %s\n", textui.StyleLabel("running:", textui.ColorEnabled()), joinCommand(command))
		result = local.RunStreaming(ctx, os.Stdout, os.Stderr, command[0], command[1:]...)
	} else {
		result = local.Run(ctx, command[0], command[1:]...)
	}
	if result.Err != nil || result.Code != 0 {
		fmt.Fprintf(os.Stderr, "homebrew trust failed: %s\n", brewOutdatedResultDetail(result, "brew trust failed"))
		return true
	}
	if report != nil {
		for index := range report.Safety {
			if report.Safety[index].Provider == "brew" {
				report.Safety[index].Evidence = appendEvidence(report.Safety[index].Evidence, "Homebrew trust command applied: "+joinCommand(command))
				break
			}
		}
		report.Report = saveLastUpdateReport(*report)
	}
	if printResult {
		fmt.Printf("%s %s\n", textui.StyleLabel("trusted:", textui.ColorEnabled()), joinCommand(command))
	}
	return true
}

type securitySkipped struct {
	Provider string   `json:"provider"`
	Kind     string   `json:"kind,omitempty"`
	Category string   `json:"category,omitempty"`
	Reason   string   `json:"reason"`
	Count    int      `json:"count"`
	Examples []string `json:"examples,omitempty"`
}

type securityFinding = securityadvisory.Finding

type osvBatchRequest = securityadvisory.OSVBatchRequest

type osvQuery = securityadvisory.OSVQuery

type osvPackage = securityadvisory.OSVPackage

type osvBatchResponse = securityadvisory.OSVBatchResponse

type osvResult = securityadvisory.OSVResult

type osvVuln = securityadvisory.OSVVuln

type osvVulnDetail = securityadvisory.OSVVulnDetail

type osvSeverity = securityadvisory.OSVSeverity

type osvAffected = securityadvisory.OSVAffected

type osvRange = securityadvisory.OSVRange

type osvRangeEvent = securityadvisory.OSVRangeEvent

type kevCatalog = securityadvisory.KEVCatalog

type kevVulnerability = securityadvisory.KEVVulnerability

type kevFinding = securityadvisory.KEVFinding

type epssResponse = securityadvisory.EPSSResponse

type epssEntry = securityadvisory.EPSSEntry

type epssFinding = securityadvisory.EPSSFinding

func runSecurity(args []string) int {
	if len(args) == 0 {
		printSecurityUsage(os.Stderr)
		return usageExitCode
	}
	command := args[0]
	args = args[1:]
	switch command {
	case "scan":
		opts, err := parseSecurityOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runSecurityScan(opts, http.DefaultClient)
	case "review":
		opts, err := parseSecurityReviewOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runSecurityReview(opts, http.DefaultClient)
	case "gate":
		opts, err := parseSecurityGateOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runSecurityGate(opts, runner.Local{})
	case "policy":
		opts, err := parseSecurityPolicyOptions(args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return usageExitCode
		}
		return runSecurityPolicy(opts)
	case "help", "--help", "-h":
		printSecurityUsage(os.Stderr)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown security command: %s\n", command)
		return usageExitCode
	}
}

func printSecurityUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: updev security <scan|review|gate|policy> ...")
	fmt.Fprintln(w, "  updev security scan [--refresh] [--provider name|all] [--ecosystem name] [--scanner auto|none|all|name[,name]] [--policy file] [--format text|json]")
	fmt.Fprintln(w, "  updev security review [--refresh] [--provider name|all] [--ecosystem name] [--scanner auto|none|all|name[,name]] [--decision allow|review|hold|block] [--kind name] [--name name] [--policy file] [--format text|json]")
	fmt.Fprintln(w, "  updev security gate [--provider brew|mise|vscode|all] [--policy file] [--format text|json]")
	fmt.Fprintln(w, "  updev security policy [list] [--state active|expired|invalid|duplicate|shadowed|needs-cleanup] [--provider name] [--kind name] [--name name] [--decision allow|review|hold|block] [--path file] [--format text|json]")
	fmt.Fprintln(w, "  updev security policy add --name name --decision allow|review|hold|block --reason text [--provider name] [--kind name] [--expires YYYY-MM-DD|--ttl-days N]")
	fmt.Fprintln(w, "  updev security policy allow|review|hold|block --name name --reason text [--provider name] [--kind name] [--expires YYYY-MM-DD|--ttl-days N]")
	fmt.Fprintln(w, "  updev security policy update --index N [--name name] [--decision allow|review|hold|block] [--reason text] [--provider name] [--kind name] [--expires YYYY-MM-DD|--ttl-days N]")
	fmt.Fprintln(w, "  updev security policy renew --index N --ttl-days N [--reason text]")
	fmt.Fprintln(w, "  updev security policy cleanup [--apply] [--path file] [--format text|json]")
	fmt.Fprintln(w, "  updev security policy remove --index N [--path file] [--format text|json]")
}

func parseSecurityOptions(args []string) (securityOptions, error) {
	opts := defaultSecurityOptions()
	fs := flag.NewFlagSet("security scan", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bindSecurityOptionFlags(fs, &opts)
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unknown option: %s", fs.Arg(0))
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if err := validateSecurityScannerOption(opts.scanner); err != nil {
		return opts, err
	}
	return opts, nil
}

func parseSecurityReviewOptions(args []string) (securityReviewOptions, error) {
	opts := securityReviewOptions{securityOptions: defaultSecurityOptions()}
	fs := flag.NewFlagSet("security review", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	bindSecurityOptionFlags(fs, &opts.securityOptions)
	fs.StringVar(&opts.decision, "decision", "", "review candidate decision filter")
	fs.StringVar(&opts.kind, "kind", "", "review candidate kind filter")
	fs.StringVar(&opts.name, "name", "", "review candidate name filter")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unknown option: %s", fs.Arg(0))
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if err := validateSecurityScannerOption(opts.scanner); err != nil {
		return opts, err
	}
	opts.decision = strings.ToLower(strings.TrimSpace(opts.decision))
	opts.kind = strings.TrimSpace(opts.kind)
	opts.name = strings.TrimSpace(opts.name)
	if opts.decision != "" && !validSecurityPolicyDecision(opts.decision) {
		return opts, fmt.Errorf("unsupported review decision: %s", opts.decision)
	}
	return opts, nil
}

func defaultSecurityOptions() securityOptions {
	return securityOptions{format: "text", root: defaultRoot(), policy: securityPolicyPath(), scanner: "auto"}
}

func bindSecurityOptionFlags(fs *flag.FlagSet, opts *securityOptions) {
	fs.StringVar(&opts.format, "format", opts.format, "output format: text or json")
	fs.StringVar(&opts.root, "root", opts.root, "chezmoi source root")
	fs.StringVar(&opts.provider, "provider", "", "provider filter")
	fs.StringVar(&opts.ecosystem, "ecosystem", "", "high-confidence ecosystem filter")
	fs.StringVar(&opts.policy, "policy", opts.policy, "security policy path")
	fs.StringVar(&opts.scanner, "scanner", opts.scanner, "external scanners: auto, none, all, or comma-separated tool names")
	fs.BoolVar(&opts.refresh, "refresh", false, "ignore cached inventory")
	fs.BoolVar(&opts.refresh, "r", false, "ignore cached inventory")
	fs.BoolVar(&opts.includeVSCode, "include-vscode", false, "include Brewfile-managed VS Code extensions")
}

func parseSecurityGateOptions(args []string) (securityGateOptions, error) {
	opts := securityGateOptions{format: "text", root: defaultRoot(), provider: "brew", policy: securityPolicyPath()}
	fs := flag.NewFlagSet("security gate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.format, "format", opts.format, "output format: text or json")
	fs.StringVar(&opts.root, "root", opts.root, "chezmoi source root")
	fs.StringVar(&opts.provider, "provider", opts.provider, "provider to gate")
	fs.StringVar(&opts.policy, "policy", opts.policy, "security policy path")
	fs.BoolVar(&opts.includeVSCode, "include-vscode", false, "include Brewfile-managed VS Code extensions when provider is all")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unknown option: %s", fs.Arg(0))
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if !strings.EqualFold(opts.provider, "brew") && !strings.EqualFold(opts.provider, "mise") && !strings.EqualFold(opts.provider, "vscode") && !strings.EqualFold(opts.provider, "all") {
		return opts, fmt.Errorf("unsupported security gate provider: %s", opts.provider)
	}
	opts.provider = strings.ToLower(opts.provider)
	return opts, nil
}

func parseSecurityPolicyOptions(args []string) (securityPolicyOptions, error) {
	opts := securityPolicyOptions{action: "list", format: "text", path: securityPolicyPath()}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		opts.action = args[0]
		args = args[1:]
	}
	requestedAction := opts.action
	fs := flag.NewFlagSet("security policy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.format, "format", opts.format, "output format: text or json")
	fs.StringVar(&opts.path, "path", opts.path, "security policy path")
	fs.StringVar(&opts.state, "state", "", "policy rule state filter")
	fs.IntVar(&opts.index, "index", 0, "1-based policy rule index")
	fs.StringVar(&opts.rule.Provider, "provider", "", "policy provider")
	fs.StringVar(&opts.rule.Kind, "kind", "", "policy kind")
	fs.StringVar(&opts.rule.Name, "name", "", "policy package or item name")
	fs.StringVar(&opts.rule.Decision, "decision", "", "policy decision: allow, review, hold, or block")
	fs.StringVar(&opts.rule.Reason, "reason", "", "policy reason")
	fs.StringVar(&opts.rule.Expires, "expires", "", "policy expiry date: YYYY-MM-DD")
	fs.IntVar(&opts.ttlDays, "ttl-days", 0, "set policy expiry to N days from today")
	fs.BoolVar(&opts.apply, "apply", false, "apply policy cleanup removals")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	opts.set = map[string]bool{}
	fs.Visit(func(f *flag.Flag) {
		opts.set[f.Name] = true
	})
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("unknown option: %s", fs.Arg(0))
	}
	if opts.format != "text" && opts.format != "json" {
		return opts, fmt.Errorf("unsupported format: %s", opts.format)
	}
	if securityPolicyDecisionAction(opts.action) {
		if opts.set["decision"] && !strings.EqualFold(opts.rule.Decision, opts.action) {
			return opts, fmt.Errorf("--decision cannot override policy %s action", opts.action)
		}
		opts.rule.Decision = opts.action
		opts.set["decision"] = true
		opts.action = "add"
	}
	if opts.action == "renew" {
		opts.action = "update"
	}
	if opts.action != "list" && opts.action != "add" && opts.action != "update" && opts.action != "remove" && opts.action != "cleanup" {
		return opts, fmt.Errorf("unsupported security policy action: %s", opts.action)
	}
	if opts.apply && opts.action != "cleanup" {
		return opts, fmt.Errorf("--apply is only supported for cleanup")
	}
	if opts.set["ttl-days"] {
		if opts.action != "add" && opts.action != "update" {
			return opts, fmt.Errorf("--ttl-days is only supported for add or update")
		}
		if opts.set["expires"] {
			return opts, fmt.Errorf("--ttl-days cannot be combined with --expires")
		}
		if opts.ttlDays <= 0 {
			return opts, fmt.Errorf("--ttl-days must be positive")
		}
		opts.rule.Expires = time.Now().AddDate(0, 0, opts.ttlDays).Format("2006-01-02")
		opts.set["expires"] = true
	}
	if opts.state != "" && !validSecurityPolicyStateFilter(opts.state) {
		return opts, fmt.Errorf("unsupported security policy state: %s", opts.state)
	}
	if requestedAction == "renew" && !opts.set["ttl-days"] {
		return opts, fmt.Errorf("renew requires --ttl-days")
	}
	if opts.action == "list" && opts.set["decision"] && !validSecurityPolicyDecision(opts.rule.Decision) {
		return opts, fmt.Errorf("unsupported security policy decision: %s", opts.rule.Decision)
	}
	return opts, nil
}

func runSecurityScan(opts securityOptions, client *http.Client) int {
	progress := startupProgress{}
	if opts.format == "text" {
		progress = newStartupProgress(os.Stdin, os.Stderr, opts.format, securityScanProgressMessage(defaultLanguage()))
	}
	progress.Start()
	report := buildSecurityReport(context.Background(), opts, client, runner.Local{})
	progress.Done()
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printSecurityText(os.Stdout, report, textui.ColorEnabled())
	}
	switch report.Status {
	case plan.StatusError:
		return 1
	case plan.StatusBlocked:
		return 3
	case plan.StatusHeld:
		return 2
	default:
		return 0
	}
}

func runSecurityReview(opts securityReviewOptions, client *http.Client) int {
	progress := startupProgress{}
	if opts.format == "text" {
		progress = newStartupProgress(os.Stdin, os.Stderr, opts.format, securityReviewProgressMessage(defaultLanguage()))
	}
	progress.Start()
	report := buildSecurityReviewReport(context.Background(), opts, client, runner.Local{})
	progress.Done()
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printSecurityReviewText(os.Stdout, report)
	}
	if report.Status == plan.StatusError {
		return 1
	}
	if len(report.Candidates) > 0 {
		return 2
	}
	return 0
}

func runSecurityGate(opts securityGateOptions, commandRunner commandRunner) int {
	report := buildSecurityGateReport(context.Background(), opts, commandRunner)
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printSecurityGateText(os.Stdout, report)
	}
	return updateExitCode(report.Status)
}

func runSecurityPolicy(opts securityPolicyOptions) int {
	report := buildSecurityPolicyReport(opts)
	if opts.format == "json" {
		if code := encodeJSON(report); code != 0 {
			return code
		}
	} else {
		printSecurityPolicyText(os.Stdout, report)
	}
	return updateExitCode(report.Status)
}

func buildSecurityPolicyReport(opts securityPolicyOptions) securityPolicyReport {
	report := securityPolicyReport{Status: plan.StatusOK, Path: opts.path}
	if opts.action == "add" {
		if err := addSecurityPolicyRule(opts.path, opts.rule); err != nil {
			report.Status = plan.StatusError
			report.Error = err.Error()
			return report
		}
	} else if opts.action == "update" {
		if err := updateSecurityPolicyRule(opts.path, opts.index, opts.rule, opts.set); err != nil {
			report.Status = plan.StatusError
			report.Error = err.Error()
			return report
		}
	} else if opts.action == "remove" {
		if err := removeSecurityPolicyRule(opts.path, opts.index); err != nil {
			report.Status = plan.StatusError
			report.Error = err.Error()
			return report
		}
	} else if opts.action == "cleanup" {
		applied, err := cleanupSecurityPolicyRules(opts)
		if err != nil {
			report.Status = plan.StatusError
			report.Error = err.Error()
			return report
		}
		report.Cleanup = applied
		if !opts.apply && len(report.Cleanup) > 0 && report.Status == plan.StatusOK {
			report.Status = plan.StatusHeld
		}
	}
	policy, err := readSecurityPolicy(opts.path)
	if err != nil {
		report.Status = plan.StatusError
		report.Error = err.Error()
		return report
	}
	views := securityPolicyRuleViews(policy)
	report.Summary = securityPolicySummaryFromViews(views)
	if opts.action == "cleanup" && len(report.Cleanup) == 0 {
		report.Cleanup = securityPolicyFilteredCleanupPlan(opts.path, views, opts, false)
		if len(report.Cleanup) > 0 && report.Status == plan.StatusOK {
			report.Status = plan.StatusHeld
		}
	}
	for _, view := range views {
		if !securityPolicyRuleMatchesStateFilter(view, securityPolicyStateFilterForAction(opts)) {
			continue
		}
		if !securityPolicyRuleMatchesListFilters(view, opts) {
			continue
		}
		report.Rules = append(report.Rules, view)
		if securityPolicyRuleNeedsCleanup(view) && report.Status == plan.StatusOK {
			report.Status = plan.StatusHeld
		}
	}
	if report.Summary != nil {
		report.Summary.FilteredRules = len(report.Rules)
	}
	return report
}

func buildSecurityGateReport(ctx context.Context, opts securityGateOptions, commandRunner commandRunner) securityGateReport {
	policyUse := loadSecurityPolicyForReportPath(opts.policy)
	report := securityGateReport{
		Status:   plan.StatusOK,
		Root:     opts.root,
		Provider: opts.provider,
		Policy:   policyUse.View(),
	}
	if len(policyUse.Warnings) > 0 {
		report.Warnings = append(report.Warnings, policyUse.Warnings...)
	}
	switch opts.provider {
	case "brew":
		report.Gates = []safetyGate{collectBrewSafetyWithPolicy(ctx, commandRunner, opts.root, policyUse.Policy)}
	case "mise":
		report.Gates = []safetyGate{collectMiseUpdateSafetyWithPolicy(ctx, commandRunner, opts.root, policyUse.Policy)}
	case "vscode":
		report.Gates = []safetyGate{collectVSCodeSafetyWithPolicy(ctx, commandRunner, opts.root, policyUse.Policy)}
	case "all":
		report.Gates = collectAllSafetyGatesWithPolicy(ctx, commandRunner, opts.root, policyUse.Policy, opts.includeVSCode || includeVSCodeExtensionsByDefault())
	default:
		report.Status = plan.StatusError
		report.Error = "unsupported security gate provider: " + opts.provider
		return report
	}
	report.Status = securityGateStatus(report.Gates)
	return report
}

func collectAllSafetyGatesWithPolicy(ctx context.Context, commandRunner commandRunner, root string, policy securityPolicy, includeVSCode bool) []safetyGate {
	if !includeVSCode {
		return []safetyGate{
			collectBrewSafetyWithPolicy(ctx, commandRunner, root, policy),
			collectMiseUpdateSafetyWithPolicy(ctx, commandRunner, root, policy),
		}
	}
	gates := make([]safetyGate, 3)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		gates[0] = collectBrewSafetyWithPolicy(ctx, commandRunner, root, policy)
	}()
	go func() {
		defer wg.Done()
		gates[1] = collectMiseUpdateSafetyWithPolicy(ctx, commandRunner, root, policy)
	}()
	go func() {
		defer wg.Done()
		gates[2] = collectVSCodeSafetyWithPolicy(ctx, commandRunner, root, policy)
	}()
	wg.Wait()
	return gates
}

func securityGateStatus(gates []safetyGate) plan.Status {
	status := plan.StatusOK
	for _, gate := range gates {
		switch gate.Status {
		case plan.StatusError:
			return plan.StatusError
		case plan.StatusHeld:
			status = plan.StatusHeld
		case plan.StatusBlocked:
			return plan.StatusBlocked
		}
	}
	return status
}

func buildSecurityReport(ctx context.Context, opts securityOptions, client *http.Client, commandRunner commandRunner) securityReport {
	includeVSCode := securityIncludesVSCode(opts)
	result := collectInventoryCachedWithOptions(ctx, opts.root, opts.refresh, inventoryCacheMaxAge, inventoryOptions{IncludeVSCode: includeVSCode})
	items := filterSecurityItems(enrichItems(result.Report.Items, legacycache.Load(), manualAppIndex(opts.root)), opts)
	packages, skipped := securityScopeFromItems(items)
	packages = filterSecurityPackages(packages, opts)
	postureItems := items
	if opts.ecosystem != "" {
		skipped = nil
		postureItems = filterSecurityItemsForEcosystem(items, opts.ecosystem)
	}
	postures := []githubPosture{}
	postureWarnings := []string{}
	brewPostures := []homebrewPosture{}
	vscodePostures := []vscodePosture{}
	npmPostures := []npmPosture{}
	cargoPostures := []cargoPosture{}
	pypiPostures := []pypiPosture{}
	var audits []nativeAudit
	scanners := []scannerEvidence{}
	homebrewAdvisoryPackages := []securityPackage{}
	vscodeAdvisoryPackages := []securityPackage{}
	policyUse := loadSecurityPolicyForReportPath(opts.policy)
	policy := policyUse.Policy
	if opts.ecosystem == "" {
		collected := collectSecurityPostures(ctx, client, opts, items, postureItems)
		postures = append(postures, collected.GitHub...)
		postureWarnings = append(postureWarnings, collected.Warnings...)
		npmPostures = collected.NPM
		npmPostures = applySecurityPolicyToNPMPostures(policy, npmPostures)
		cargoPostures = collected.Cargo
		cargoPostures = applySecurityPolicyToCargoPostures(policy, cargoPostures)
		pypiPostures = collected.PyPI
		pypiPostures = applySecurityPolicyToPyPIPostures(policy, pypiPostures)
		brewPostures = collected.Brew
		vscodePostures = collected.VSCode
		includeBrew := securityScanIncludesBrewProvider(opts.provider)
		if includeBrew {
			brewPostures = applySecurityPolicyToHomebrewPostures(policy, brewPostures)
			homebrewAdvisoryPackages = homebrewAdvisoryPackagesFromPostures(brewPostures)
		}
		if includeVSCode {
			vscodePostures = applySecurityPolicyToVSCodePostures(policy, vscodePostures)
			vscodeAdvisoryPackages = vscodeAdvisoryPackagesFromPostures(vscodePostures)
		}
		githubCollected := collectSecurityDerivedGitHubPostures(ctx, client, includeBrew, includeVSCode, npmPostures, cargoPostures, pypiPostures, brewPostures, vscodePostures)
		postures = append(postures, githubCollected.Postures...)
		postureWarnings = append(postureWarnings, githubCollected.Warnings...)
		postures = applySecurityPolicyToGitHubPostures(policy, postures)
	} else {
		var err error
		switch strings.ToLower(opts.ecosystem) {
		case "npm":
			npmPostures, err = registryaudit.NPMPosturesFromItems(ctx, client, npmRegistryURL(), postureItems)
			if err != nil {
				postureWarnings = append(postureWarnings, "npm registry posture failed: "+err.Error())
			}
			npmPostures = applySecurityPolicyToNPMPostures(policy, npmPostures)
		case "crates.io":
			cargoPostures, err = registryaudit.CargoPosturesFromItems(ctx, client, cratesIOAPIURL(), postureItems)
			if err != nil {
				postureWarnings = append(postureWarnings, "crates.io posture failed: "+err.Error())
			}
			cargoPostures = applySecurityPolicyToCargoPostures(policy, cargoPostures)
		case "pypi":
			pypiPostures, err = registryaudit.PyPIPosturesFromItems(ctx, client, pypiAPIURL(), postureItems)
			if err != nil {
				postureWarnings = append(postureWarnings, "PyPI posture failed: "+err.Error())
			}
			pypiPostures = applySecurityPolicyToPyPIPostures(policy, pypiPostures)
		}
		registryGitHubPostures, err := githubPosturesFromRegistry(ctx, client, githubAPIURL(), npmPostures, cargoPostures, pypiPostures)
		if err != nil {
			postureWarnings = append(postureWarnings, "registry GitHub repo posture failed: "+err.Error())
		}
		postures = applySecurityPolicyToGitHubPostures(policy, registryGitHubPostures)
	}
	packages = annotateSecurityPackagePath(packages, npmPostures)
	audits = nativeAuditsFromPackages(ctx, commandRunner, packages, opts)
	if securityScanIncludesProjectProvider(opts.provider) {
		scanners = scannerEvidenceFromOptions(ctx, commandRunner, opts, packages)
	}
	scanners = applySecurityPolicyToScanners(policy, scanners)
	report := securityReport{
		Status:   plan.StatusOK,
		Root:     opts.root,
		Source:   "osv",
		Sources:  securitySources(postures, brewPostures, vscodePostures, npmPostures, cargoPostures, pypiPostures, audits, scanners),
		Policy:   policyUse.View(),
		Scanned:  packages,
		Posture:  postures,
		Brew:     brewPostures,
		VSCode:   vscodePostures,
		NPM:      npmPostures,
		Cargo:    cargoPostures,
		PyPI:     pypiPostures,
		Audits:   audits,
		Scanners: scanners,
		Skipped:  skipped,
	}
	report.Status = securityPostureStatus(report.Status, report.Posture, report.Brew, report.VSCode, report.NPM, report.Cargo, report.PyPI)
	report.Status = nativeAuditReportStatus(report.Status, report.Audits)
	report.Status = scannerEvidenceReportStatus(report.Status, report.Scanners)
	report.Warnings = append(report.Warnings, postureWarnings...)
	if len(policyUse.Warnings) > 0 {
		report.Warnings = append(report.Warnings, policyUse.Warnings...)
	}
	if len(packages) == 0 && len(homebrewAdvisoryPackages) == 0 && len(vscodeAdvisoryPackages) == 0 {
		return report
	}
	findings, advisoryWarnings, err := collectSecurityAdvisoryFindings(ctx, client, packages, homebrewAdvisoryPackages, vscodeAdvisoryPackages)
	if err != nil {
		report.Status = plan.StatusError
		report.Error = err.Error()
		return report
	}
	report.Warnings = append(report.Warnings, advisoryWarnings...)
	enriched, kevErr := enrichFindingsWithKEV(ctx, client, cisaKEVURL(), findings)
	if kevErr != nil {
		report.Warnings = append(report.Warnings, "CISA KEV enrichment failed: "+kevErr.Error())
	} else {
		findings = enriched
	}
	enriched, epssErr := enrichFindingsWithEPSS(ctx, client, epssURL(), findings)
	if epssErr != nil {
		report.Warnings = append(report.Warnings, "FIRST EPSS enrichment failed: "+epssErr.Error())
	} else {
		findings = enriched
	}
	findings = applySecurityPolicyToFindings(policy, findings)
	sortSecurityFindings(findings)
	report.Findings = findings
	if len(findings) > 0 {
		for _, finding := range findings {
			report.Status = securityStatusFromFinding(report.Status, finding)
		}
	}
	return report
}

type securityPostureCollection struct {
	GitHub   []githubPosture
	Brew     []homebrewPosture
	VSCode   []vscodePosture
	NPM      []npmPosture
	Cargo    []cargoPosture
	PyPI     []pypiPosture
	Warnings []string
}

type securityDerivedGitHubCollection struct {
	Postures []githubPosture
	Warnings []string
}

func collectSecurityPostures(ctx context.Context, client *http.Client, opts securityOptions, items []plan.Item, postureItems []plan.Item) securityPostureCollection {
	var githubPostures []githubPosture
	var brewPostures []homebrewPosture
	var vscodePostures []vscodePosture
	var npmPostures []npmPosture
	var cargoPostures []cargoPosture
	var pypiPostures []pypiPosture
	var githubErr, npmErr, cargoErr, pypiErr, brewErr, vscodeErr error
	includeBrew := securityScanIncludesBrewProvider(opts.provider)
	includeVSCode := securityIncludesVSCode(opts)
	var wg sync.WaitGroup
	wg.Add(4)
	go func() {
		defer wg.Done()
		githubPostures, githubErr = githubPosturesFromItems(ctx, client, githubAPIURL(), items)
	}()
	go func() {
		defer wg.Done()
		npmPostures, npmErr = registryaudit.NPMPosturesFromItems(ctx, client, npmRegistryURL(), postureItems)
	}()
	go func() {
		defer wg.Done()
		cargoPostures, cargoErr = registryaudit.CargoPosturesFromItems(ctx, client, cratesIOAPIURL(), postureItems)
	}()
	go func() {
		defer wg.Done()
		pypiPostures, pypiErr = registryaudit.PyPIPosturesFromItems(ctx, client, pypiAPIURL(), postureItems)
	}()
	if includeBrew {
		wg.Add(1)
		go func() {
			defer wg.Done()
			brewPostures, brewErr = homebrewPosturesFromItems(ctx, client, homebrewAPIURL(), opts.root, items)
		}()
	}
	if includeVSCode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vscodePostures, vscodeErr = vscodePosturesFromItems(ctx, client, vscodeMarketplaceURL(), items)
		}()
	}
	wg.Wait()
	collected := securityPostureCollection{
		GitHub: githubPostures,
		Brew:   brewPostures,
		VSCode: vscodePostures,
		NPM:    npmPostures,
		Cargo:  cargoPostures,
		PyPI:   pypiPostures,
	}
	collected.Warnings = appendWarningIfErr(collected.Warnings, "GitHub repo posture failed", githubErr)
	collected.Warnings = appendWarningIfErr(collected.Warnings, "npm registry posture failed", npmErr)
	collected.Warnings = appendWarningIfErr(collected.Warnings, "crates.io posture failed", cargoErr)
	collected.Warnings = appendWarningIfErr(collected.Warnings, "PyPI posture failed", pypiErr)
	collected.Warnings = appendWarningIfErr(collected.Warnings, "Homebrew metadata posture failed", brewErr)
	collected.Warnings = appendWarningIfErr(collected.Warnings, "VS Code marketplace posture failed", vscodeErr)
	return collected
}

func collectSecurityDerivedGitHubPostures(ctx context.Context, client *http.Client, includeBrew bool, includeVSCode bool, npmPostures []npmPosture, cargoPostures []cargoPosture, pypiPostures []pypiPosture, brewPostures []homebrewPosture, vscodePostures []vscodePosture) securityDerivedGitHubCollection {
	var collected securityDerivedGitHubCollection
	var registryPostures []githubPosture
	var brewGitHubPostures []githubPosture
	var vscodeGitHubPostures []githubPosture
	var registryErr, brewErr, vscodeErr error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		registryPostures, registryErr = githubPosturesFromRegistry(ctx, client, githubAPIURL(), npmPostures, cargoPostures, pypiPostures)
	}()
	if includeBrew {
		wg.Add(1)
		go func() {
			defer wg.Done()
			brewGitHubPostures, brewErr = githubPosturesFromHomebrew(ctx, client, githubAPIURL(), brewPostures)
		}()
	}
	if includeVSCode {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vscodeGitHubPostures, vscodeErr = githubPosturesFromVSCode(ctx, client, githubAPIURL(), vscodePostures)
		}()
	}
	wg.Wait()
	collected.Postures = append(collected.Postures, registryPostures...)
	collected.Postures = append(collected.Postures, brewGitHubPostures...)
	collected.Postures = append(collected.Postures, vscodeGitHubPostures...)
	collected.Warnings = appendWarningIfErr(collected.Warnings, "registry GitHub repo posture failed", registryErr)
	collected.Warnings = appendWarningIfErr(collected.Warnings, "Homebrew GitHub repo posture failed", brewErr)
	collected.Warnings = appendWarningIfErr(collected.Warnings, "VS Code GitHub repo posture failed", vscodeErr)
	return collected
}

func collectSecurityAdvisoryFindings(ctx context.Context, client *http.Client, packages []securityPackage, homebrewAdvisoryPackages []securityPackage, vscodeAdvisoryPackages []securityPackage) ([]securityFinding, []string, error) {
	var packageOSV []securityFinding
	var packageGitHub []securityFinding
	var homebrewOSV []securityFinding
	var homebrewGitHub []securityFinding
	var vscodeOSV []securityFinding
	var packageOSVErr, packageGitHubErr, homebrewOSVErr, homebrewGitHubErr, vscodeOSVErr error
	var wg sync.WaitGroup
	if len(packages) > 0 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			packageOSV, packageOSVErr = queryOSVBatch(ctx, client, osvAPIURL(), packages)
		}()
		go func() {
			defer wg.Done()
			packageGitHub, packageGitHubErr = queryGitHubAdvisories(ctx, client, githubAPIURL(), packages)
		}()
	}
	if len(homebrewAdvisoryPackages) > 0 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			homebrewOSV, homebrewOSVErr = queryOSVBatch(ctx, client, osvAPIURL(), homebrewAdvisoryPackages)
		}()
		go func() {
			defer wg.Done()
			homebrewGitHub, homebrewGitHubErr = queryGitHubAdvisories(ctx, client, githubAPIURL(), homebrewAdvisoryPackages)
		}()
	}
	if len(vscodeAdvisoryPackages) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			vscodeOSV, vscodeOSVErr = queryOSVBatch(ctx, client, osvAPIURL(), vscodeAdvisoryPackages)
		}()
	}
	wg.Wait()
	if packageOSVErr != nil {
		return nil, nil, packageOSVErr
	}
	findings := []securityFinding{}
	findings = append(findings, packageOSV...)
	findings = appendUniqueSecurityFindings(findings, packageGitHub...)
	findings = append(findings, homebrewOSV...)
	findings = appendUniqueSecurityFindings(findings, homebrewGitHub...)
	findings = append(findings, vscodeOSV...)
	warnings := []string{}
	warnings = appendWarningIfErr(warnings, "GitHub Advisory query failed", packageGitHubErr)
	warnings = appendWarningIfErr(warnings, "Homebrew advisory OSV query failed", homebrewOSVErr)
	warnings = appendWarningIfErr(warnings, "Homebrew advisory GitHub query failed", homebrewGitHubErr)
	warnings = appendWarningIfErr(warnings, "VS Code advisory OSV query failed", vscodeOSVErr)
	return findings, warnings, nil
}

func appendWarningIfErr(warnings []string, prefix string, err error) []string {
	if err == nil {
		return warnings
	}
	return append(warnings, prefix+": "+err.Error())
}

func filterSecurityItems(items []plan.Item, opts securityOptions) []plan.Item {
	if opts.provider == "" || securityProviderIsAll(opts.provider) {
		return items
	}
	out := make([]plan.Item, 0, len(items))
	for _, item := range items {
		if providerFilterIsVSCode(opts.provider) {
			if item.Kind == "vscode" {
				out = append(out, item)
			}
			continue
		}
		if strings.EqualFold(item.Provider, opts.provider) {
			out = append(out, item)
		}
	}
	return out
}

func securityProviderIsAll(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), "all")
}

func securityScanIncludesBrewProvider(provider string) bool {
	return securityProviderIsAll(provider) || strings.EqualFold(strings.TrimSpace(provider), "brew")
}

func securityIncludesVSCode(opts securityOptions) bool {
	return opts.includeVSCode || includeVSCodeExtensionsByDefault() || providerFilterIsVSCode(opts.provider)
}

func securityScanIncludesProjectProvider(provider string) bool {
	trimmed := strings.TrimSpace(provider)
	return trimmed == "" || securityProviderIsAll(trimmed) || strings.EqualFold(trimmed, "project")
}

func filterSecurityPackages(packages []securityPackage, opts securityOptions) []securityPackage {
	if opts.ecosystem == "" {
		return packages
	}
	out := make([]securityPackage, 0, len(packages))
	for _, pkg := range packages {
		if strings.EqualFold(pkg.Ecosystem, opts.ecosystem) {
			out = append(out, pkg)
		}
	}
	return out
}

func filterSecurityItemsForEcosystem(items []plan.Item, ecosystem string) []plan.Item {
	out := make([]plan.Item, 0, len(items))
	for _, item := range items {
		pkg, ok := securityPackageFromItem(item)
		if !ok {
			continue
		}
		if strings.EqualFold(pkg.Ecosystem, ecosystem) {
			out = append(out, item)
		}
	}
	return out
}

func securityPackagesFromItems(items []plan.Item) []securityPackage {
	packages, skipped := securityScopeFromItems(items)
	_ = skipped // This helper intentionally exposes only advisory package targets.
	return packages
}

func securityScopeFromItems(items []plan.Item) ([]securityPackage, []securitySkipped) {
	packages := []securityPackage{}
	skipped := map[string]securitySkipped{}
	for _, item := range items {
		pkg, ok := securityPackageFromItem(item)
		if ok {
			packages = append(packages, pkg)
			continue
		}
		if reason := securitySkipReason(item); reason != "" {
			key := strings.Join([]string{item.Provider, item.Kind, item.Category, reason}, "\x00")
			entry := skipped[key]
			entry.Provider = item.Provider
			entry.Kind = item.Kind
			entry.Category = item.Category
			entry.Reason = reason
			entry.Count++
			entry.Examples = appendSecuritySkippedExample(entry.Examples, item.Name)
			skipped[key] = entry
		}
	}
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Ecosystem != packages[j].Ecosystem {
			return packages[i].Ecosystem < packages[j].Ecosystem
		}
		return packages[i].Package < packages[j].Package
	})
	skippedList := make([]securitySkipped, 0, len(skipped))
	for _, entry := range skipped {
		skippedList = append(skippedList, entry)
	}
	sort.Slice(skippedList, func(i, j int) bool {
		left := skippedList[i]
		right := skippedList[j]
		if left.Provider != right.Provider {
			return left.Provider < right.Provider
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Category != right.Category {
			return left.Category < right.Category
		}
		return left.Reason < right.Reason
	})
	return packages, skippedList
}

func appendSecuritySkippedExample(examples []string, name string) []string {
	name = strings.TrimSpace(name)
	if name == "" || len(examples) >= 3 {
		return examples
	}
	for _, existing := range examples {
		if existing == name {
			return examples
		}
	}
	return append(examples, name)
}

func securitySources(postures []githubPosture, brewPostures []homebrewPosture, vscodePostures []vscodePosture, npmPostures []npmPosture, cargoPostures []cargoPosture, pypiPostures []pypiPosture, audits []nativeAudit, scanners []scannerEvidence) []string {
	sources := []string{"osv", "github-advisory", "cisa-kev", "first-epss"}
	if len(postures) > 0 {
		sources = append(sources, "github-repo")
	}
	if len(brewPostures) > 0 {
		sources = append(sources, "homebrew-api")
	}
	if len(vscodePostures) > 0 {
		sources = append(sources, "vscode-marketplace")
	}
	if len(npmPostures) > 0 {
		sources = append(sources, "npm-registry")
	}
	if len(cargoPostures) > 0 {
		sources = append(sources, "crates-io")
	}
	if len(pypiPostures) > 0 {
		sources = append(sources, "pypi")
	}
	if len(audits) > 0 {
		sources = append(sources, "provider-native-audit")
	}
	for _, scanner := range scanners {
		if scanner.Tool != "" && !stringSliceContains(sources, scanner.Tool) {
			sources = append(sources, scanner.Tool)
		}
	}
	return sources
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func securityPostureStatus(current plan.Status, postures []githubPosture, brewPostures []homebrewPosture, vscodePostures []vscodePosture, npmPostures []npmPosture, cargoPostures []cargoPosture, pypiPostures []pypiPosture) plan.Status {
	status := current
	for _, posture := range postures {
		status = securityStatusFromDecision(status, posture.Decision)
	}
	for _, posture := range brewPostures {
		status = securityStatusFromDecision(status, posture.Decision)
	}
	for _, posture := range vscodePostures {
		status = securityStatusFromDecision(status, posture.Decision)
	}
	for _, posture := range npmPostures {
		status = securityStatusFromDecision(status, posture.Decision)
	}
	for _, posture := range cargoPostures {
		status = securityStatusFromDecision(status, posture.Decision)
	}
	for _, posture := range pypiPostures {
		status = securityStatusFromDecision(status, posture.Decision)
	}
	return status
}

func securityStatusFromDecision(current plan.Status, decision string) plan.Status {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "block":
		return plan.StatusBlocked
	case "hold", "review":
		if current == plan.StatusOK {
			return plan.StatusHeld
		}
	}
	return current
}

func securityStatusFromFinding(current plan.Status, finding securityFinding) plan.Status {
	if finding.Status == plan.StatusBlocked || strings.EqualFold(finding.Decision, "block") {
		return plan.StatusBlocked
	}
	if finding.Status == plan.StatusHeld || strings.EqualFold(finding.Decision, "hold") || strings.EqualFold(finding.Decision, "review") {
		if current == plan.StatusOK {
			return plan.StatusHeld
		}
	}
	return current
}

func securityPackageFromItem(item plan.Item) (securityPackage, bool) {
	if item.Provider != "mise" || item.Version == "" {
		return securityPackage{}, false
	}
	ecosystem := ""
	name := ""
	switch {
	case strings.HasPrefix(item.Name, "npm:"):
		ecosystem = "npm"
		name = strings.TrimPrefix(item.Name, "npm:")
	case strings.HasPrefix(item.Name, "cargo:"):
		ecosystem = "crates.io"
		name = strings.TrimPrefix(item.Name, "cargo:")
	case strings.HasPrefix(item.Name, "pipx:"):
		ecosystem = "PyPI"
		name = strings.TrimPrefix(item.Name, "pipx:")
	default:
		return securityPackage{}, false
	}
	if name == "" {
		return securityPackage{}, false
	}
	return securityPackage{
		Provider:   item.Provider,
		Name:       item.Name,
		Version:    item.Version,
		Ecosystem:  ecosystem,
		Package:    name,
		Confidence: "high",
	}, true
}

func annotateSecurityPackagePath(packages []securityPackage, npmPostures []npmPosture) []securityPackage {
	npmBinaries := npmBinariesByPackage(npmPostures)
	out := make([]securityPackage, 0, len(packages))
	for _, pkg := range packages {
		binaries := securityPackageBinaryCandidates(pkg, npmBinaries)
		if len(binaries) == 0 {
			pkg.PathState = "unknown"
			out = append(out, pkg)
			continue
		}
		pkg.BinaryName = strings.Join(binaries, ",")
		pkg.PathState = "not-found"
		for _, binary := range binaries {
			path, err := exec.LookPath(binary)
			if err == nil {
				pkg.BinaryName = binary
				pkg.BinaryPath = path
				pkg.PathState = "on-path"
				break
			}
		}
		out = append(out, pkg)
	}
	return out
}

func npmBinariesByPackage(postures []npmPosture) map[string][]string {
	out := map[string][]string{}
	for _, posture := range postures {
		if len(posture.Binaries) == 0 {
			continue
		}
		out[strings.ToLower(posture.Package)] = append([]string(nil), posture.Binaries...)
	}
	return out
}

func securityPackageBinaryCandidates(pkg securityPackage, npmBinaries map[string][]string) []string {
	name := strings.TrimSpace(pkg.Package)
	if name == "" {
		return nil
	}
	switch strings.ToLower(pkg.Ecosystem) {
	case "npm":
		if binaries := npmBinaries[strings.ToLower(name)]; len(binaries) > 0 {
			return prioritizeSecurityBinaryCandidates(name, binaries)
		}
		if strings.Contains(name, "/") || strings.HasPrefix(name, "@") {
			return nil
		}
		return []string{name}
	case "pypi":
		if strings.ContainsAny(name, "/@:") {
			return nil
		}
		return []string{name}
	case "crates.io":
		return nativeaudit.CargoBinaryCandidates(name)
	default:
		return nil
	}
}

func cargoBinaryCandidates(crate string) []string {
	if strings.ContainsAny(crate, "/@:") {
		return nil
	}
	switch crate {
	case "fd-find":
		return []string{"fd", "fd-find"}
	case "git-delta":
		return []string{"delta", "git-delta"}
	default:
		return []string{crate}
	}
}

func prioritizeSecurityBinaryCandidates(pkg string, binaries []string) []string {
	out := append([]string(nil), binaries...)
	preferred := registryaudit.NPMDefaultBinaryName(pkg)
	for index, binary := range out {
		if binary != preferred {
			continue
		}
		copy(out[1:index+1], out[0:index])
		out[0] = binary
		break
	}
	return out
}

func securitySkipReason(item plan.Item) string {
	if item.Provider == "mise" {
		switch {
		case strings.HasPrefix(item.Name, "npm:"), strings.HasPrefix(item.Name, "cargo:"), strings.HasPrefix(item.Name, "pipx:"):
			if item.Version == "" {
				return "missing version for high-confidence ecosystem"
			}
			return ""
		case strings.HasPrefix(item.Name, "github:"):
			return ""
		case strings.HasPrefix(item.Name, "aqua:"), strings.HasPrefix(item.Name, "vfox:"):
			return "unsupported mise backend ecosystem"
		default:
			return "no direct OSV ecosystem mapping"
		}
	}
	if item.Provider == "brew" {
		if item.Kind == "vscode" {
			return "vscode extensions require marketplace advisory mapping"
		}
		return "homebrew requires curated advisory mapping"
	}
	return "unsupported provider"
}

func queryOSVBatch(ctx context.Context, client *http.Client, apiURL string, packages []securityPackage) ([]securityFinding, error) {
	return securityadvisory.QueryOSVBatch(ctx, client, apiURL, packages)
}

func queryGitHubAdvisories(ctx context.Context, client *http.Client, apiBase string, packages []securityPackage) ([]securityFinding, error) {
	return securityadvisory.QueryGitHubAdvisories(ctx, client, apiBase, githubToken(), packages)
}

func isGitHubAdvisoryFinding(finding securityFinding) bool {
	return securityadvisory.IsGitHubAdvisoryFinding(finding)
}

func appendUniqueSecurityFindings(findings []securityFinding, additions ...securityFinding) []securityFinding {
	return securityadvisory.AppendUniqueFindings(findings, additions...)
}

func enrichFindingsWithKEV(ctx context.Context, client *http.Client, kevURL string, findings []securityFinding) ([]securityFinding, error) {
	return securityadvisory.EnrichWithKEV(ctx, client, kevURL, findings)
}

func enrichFindingsWithEPSS(ctx context.Context, client *http.Client, epssURL string, findings []securityFinding) ([]securityFinding, error) {
	return securityadvisory.EnrichWithEPSS(ctx, client, epssURL, findings)
}

func securityExposureFromPackage(pkg securityPackage) string {
	return securityadvisory.ExposureFromPackage(pkg)
}

func securityFindingReason(finding securityFinding) string {
	return securityadvisory.Reason(finding)
}

func securityFindingRemediation(finding securityFinding) string {
	return securityadvisory.Remediation(finding)
}

func sortSecurityFindings(findings []securityFinding) {
	securityadvisory.SortFindings(findings)
}

func primaryOSVSeverity(severities []osvSeverity) string {
	return securityadvisory.PrimaryOSVSeverity(severities)
}

func fixedVersionsFromOSVDetail(detail osvVulnDetail, pkg securityPackage) []string {
	return securityadvisory.FixedVersionsFromOSVDetail(detail, pkg)
}

func printSecurityText(w io.Writer, report securityReport, color bool) {
	fmt.Fprintf(w, "%s %s\n", textui.Style("updev security scan", "\033[1m", color), textui.StyleStatus(string(report.Status), color))
	fmt.Fprintf(w, "root: %s\n", report.Root)
	fmt.Fprintf(w, "sources: %s\n", strings.Join(report.Sources, ","))
	if report.Policy != nil && report.Policy.Loaded {
		fmt.Fprintf(w, "policy: %s (%s)\n", report.Policy.Path, securityPolicyUseSummary(report.Policy))
	}
	fmt.Fprintf(w, "scanned: %d high-confidence packages\n", len(report.Scanned))
	if len(report.Posture) > 0 {
		reviewCount := githubPostureReviewCount(report.Posture)
		fmt.Fprintf(w, "github posture: %d repos checked", len(report.Posture))
		if reviewCount > 0 {
			fmt.Fprintf(w, ", %d need attention", reviewCount)
		}
		fmt.Fprintln(w)
	}
	if len(report.Brew) > 0 {
		reviewCount := homebrewPostureReviewCount(report.Brew)
		fmt.Fprintf(w, "homebrew posture: %d entries checked", len(report.Brew))
		if reviewCount > 0 {
			fmt.Fprintf(w, ", %d need attention", reviewCount)
		}
		fmt.Fprintln(w)
	}
	if len(report.VSCode) > 0 {
		reviewCount := vscodePostureReviewCount(report.VSCode)
		fmt.Fprintf(w, "vscode posture: %d extensions checked", len(report.VSCode))
		if reviewCount > 0 {
			fmt.Fprintf(w, ", %d need attention", reviewCount)
		}
		fmt.Fprintln(w)
	}
	if len(report.NPM) > 0 {
		reviewCount := registryaudit.NPMPostureReviewCount(report.NPM)
		fmt.Fprintf(w, "npm posture: %d packages checked", len(report.NPM))
		if reviewCount > 0 {
			fmt.Fprintf(w, ", %d need attention", reviewCount)
		}
		fmt.Fprintln(w)
	}
	if len(report.Cargo) > 0 {
		reviewCount := registryaudit.CargoPostureReviewCount(report.Cargo)
		fmt.Fprintf(w, "cargo posture: %d crates checked", len(report.Cargo))
		if reviewCount > 0 {
			fmt.Fprintf(w, ", %d need attention", reviewCount)
		}
		fmt.Fprintln(w)
	}
	if len(report.PyPI) > 0 {
		reviewCount := registryaudit.PyPIPostureReviewCount(report.PyPI)
		fmt.Fprintf(w, "pypi posture: %d packages checked", len(report.PyPI))
		if reviewCount > 0 {
			fmt.Fprintf(w, ", %d need attention", reviewCount)
		}
		fmt.Fprintln(w)
	}
	if len(report.Audits) > 0 {
		held, unavailable, errors := nativeAuditSummary(report.Audits)
		fmt.Fprintf(w, "native audits: %d checks", len(report.Audits))
		if held > 0 {
			fmt.Fprintf(w, ", %d held", held)
		}
		if unavailable > 0 {
			fmt.Fprintf(w, ", %d unavailable", unavailable)
		}
		if errors > 0 {
			fmt.Fprintf(w, ", %d errors", errors)
		}
		fmt.Fprintln(w)
	}
	if len(report.Scanners) > 0 {
		held, unavailable, errors := scannerEvidenceSummary(report.Scanners)
		fmt.Fprintf(w, "scanners: %d checks", len(report.Scanners))
		if held > 0 {
			fmt.Fprintf(w, ", %d held", held)
		}
		if unavailable > 0 {
			fmt.Fprintf(w, ", %d unavailable", unavailable)
		}
		if errors > 0 {
			fmt.Fprintf(w, ", %d errors", errors)
		}
		fmt.Fprintln(w)
	}
	if len(report.Skipped) > 0 {
		fmt.Fprintf(w, "skipped: %d groups outside automatic matching\n", len(report.Skipped))
	}
	if report.Error != "" {
		fmt.Fprintf(w, "error: %s\n", report.Error)
		return
	}
	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, "\nwarnings")
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "  %s\n", warning)
		}
	}
	if hasNativeAuditAttention(report.Audits) {
		fmt.Fprintln(w, "\nnative audits")
		rows := make([][]string, 0, len(report.Audits))
		for _, audit := range report.Audits {
			if !securityDecisionNeedsAttention(audit.Decision) && audit.Status == plan.StatusOK {
				continue
			}
			rows = append(rows, []string{
				audit.Ecosystem,
				audit.Tool,
				audit.Target,
				string(audit.Status),
				audit.Decision,
				nativeAuditIssue(audit),
				localizedNativeAuditReason(audit),
				audit.Error,
			})
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "ecosystem", Min: 8, Max: 12},
			{Header: "tool", Min: 4, Max: 8},
			{Header: "target", Min: 0, Max: 24},
			{Header: "status", Min: 8, Max: 12},
			{Header: "decision", Min: 8, Max: 10},
			{Header: "issue", Min: 0, Max: 18},
			{Header: "reason", Min: 16, Max: 48},
			{Header: "detail", Min: 0, Max: 56},
		}, rows, color)
	}
	if len(report.Skipped) > 0 {
		fmt.Fprintln(w, "\nskipped automatic matching")
		rows := make([][]string, 0, len(report.Skipped))
		for _, entry := range report.Skipped {
			rows = append(rows, []string{
				entry.Provider,
				entry.Kind,
				entry.Category,
				strconv.Itoa(entry.Count),
				strings.Join(entry.Examples, ","),
				localizedSecurityReason(entry.Reason),
			})
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "provider", Min: 4, Max: 10},
			{Header: "kind", Min: 0, Max: 10},
			{Header: "category", Min: 0, Max: 12},
			{Header: "count", Min: 5, Max: 5},
			{Header: "examples", Min: 0, Max: 32},
			{Header: "reason", Min: 20, Max: 64},
		}, rows, color)
	}
	if hasScannerEvidenceAttention(report.Scanners) {
		fmt.Fprintln(w, "\nscanners")
		rows := make([][]string, 0, len(report.Scanners))
		for _, scanner := range report.Scanners {
			if !securityDecisionNeedsAttention(scanner.Decision) && scanner.Status == plan.StatusOK {
				continue
			}
			rows = append(rows, []string{
				scanner.Tool,
				string(scanner.Status),
				scanner.Decision,
				strconv.Itoa(scanner.FindingCount),
				scannerEvidenceIssue(scanner),
				localizedSecurityReason(scanner.Reason),
			})
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "tool", Min: 8, Max: 12},
			{Header: "status", Min: 8, Max: 12},
			{Header: "decision", Min: 8, Max: 10},
			{Header: "finds", Min: 5, Max: 8},
			{Header: "issue", Min: 0, Max: 18},
			{Header: "reason", Min: 16, Max: 48},
		}, rows, color)
	}
	if hasScannerFindings(report.Scanners) {
		fmt.Fprintln(w, "\nscanner findings")
		rows := make([][]string, 0, 12)
		nextSteps := []string{}
		total := 0
		for _, scanner := range report.Scanners {
			for _, finding := range scanner.Findings {
				if !securityDecisionNeedsAttention(finding.Decision) {
					continue
				}
				total++
				if len(rows) == 12 {
					continue
				}
				if finding.Remediation != "" && len(nextSteps) < 6 {
					nextSteps = append(nextSteps, fmt.Sprintf("%s %s: %s", scanner.Tool, scannerFindingID(finding), truncate(oneLine(localizedSecurityRemediation(finding.Remediation)), 120)))
				}
				rows = append(rows, []string{
					scanner.Tool,
					finding.Kind,
					finding.DependencyKind,
					scannerFindingSource(finding),
					scannerFindingID(finding),
					scannerFindingDetail(finding),
					finding.Severity,
					strings.Join(finding.FixedVersions, ","),
				})
			}
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "tool", Min: 8, Max: 12},
			{Header: "kind", Min: 6, Max: 13},
			{Header: "dep", Min: 0, Max: 10},
			{Header: "source", Min: 16, Max: 36},
			{Header: "id", Min: 10, Max: 22},
			{Header: "detail", Min: 8, Max: 24},
			{Header: "severity", Min: 8, Max: 16},
			{Header: "fixed", Min: 0, Max: 18},
		}, rows, color)
		if remaining := total - len(rows); remaining > 0 {
			fmt.Fprintf(w, "  ... %d more scanner findings\n", remaining)
		}
		if len(nextSteps) > 0 {
			fmt.Fprintln(w, "\nscanner next steps")
			for _, nextStep := range nextSteps {
				fmt.Fprintf(w, "  %s\n", nextStep)
			}
		}
	}
	if hasGitHubPostureReview(report.Posture) {
		fmt.Fprintln(w, "\ngithub posture")
		rows := make([][]string, 0, len(report.Posture))
		nextSteps := []string{}
		for _, posture := range report.Posture {
			if !securityDecisionNeedsAttention(posture.Decision) {
				continue
			}
			if posture.Remediation != "" && len(nextSteps) < 6 {
				nextSteps = append(nextSteps, fmt.Sprintf("%s: %s", posture.Repository, truncate(oneLine(localizedSecurityRemediation(posture.Remediation)), 120)))
			}
			rows = append(rows, []string{
				posture.Repository,
				posture.DefaultBranch,
				posture.PushedAt,
				posture.Decision,
				localizedGitHubPostureReason(posture),
			})
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "repository", Min: 16, Max: 34},
			{Header: "branch", Min: 6, Max: 12},
			{Header: "pushed", Min: 10, Max: 20},
			{Header: "decision", Min: 8, Max: 10},
			{Header: "reason", Min: 16, Max: 32},
		}, rows, color)
		printNextSteps(w, "github posture next steps", nextSteps)
	}
	if hasHomebrewPostureReview(report.Brew) {
		fmt.Fprintln(w, "\nhomebrew posture")
		rows := make([][]string, 0, len(report.Brew))
		nextSteps := []string{}
		for _, posture := range report.Brew {
			if !securityDecisionNeedsAttention(posture.Decision) {
				continue
			}
			if posture.Remediation != "" && len(nextSteps) < 6 {
				nextSteps = append(nextSteps, fmt.Sprintf("%s %s: %s", posture.Kind, posture.Name, truncate(oneLine(localizedSecurityRemediation(posture.Remediation)), 120)))
			}
			rows = append(rows, []string{
				posture.Kind,
				posture.Name,
				posture.Tap,
				firstNonEmpty(posture.TrustStatus, "-"),
				posture.Decision,
				localizedHomebrewPostureReason(posture),
			})
			if len(rows) == 12 {
				break
			}
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "kind", Min: 4, Max: 8},
			{Header: "name", Min: 12, Max: 28},
			{Header: "tap", Min: 8, Max: 18},
			{Header: "trust", Min: 6, Max: 12},
			{Header: "decision", Min: 8, Max: 10},
			{Header: "reason", Min: 18, Max: 40},
		}, rows, color)
		if remaining := homebrewPostureReviewCount(report.Brew) - len(rows); remaining > 0 {
			fmt.Fprintf(w, "  ... %d more review entries\n", remaining)
		}
		printNextSteps(w, "homebrew posture next steps", nextSteps)
	}
	if hasVSCodePostureReview(report.VSCode) {
		fmt.Fprintln(w, "\nvscode posture")
		rows := make([][]string, 0, len(report.VSCode))
		nextSteps := []string{}
		for _, posture := range report.VSCode {
			if !securityDecisionNeedsAttention(posture.Decision) {
				continue
			}
			if posture.Remediation != "" && len(nextSteps) < 6 {
				nextSteps = append(nextSteps, fmt.Sprintf("%s: %s", posture.Name, truncate(oneLine(localizedSecurityRemediation(posture.Remediation)), 120)))
			}
			rows = append(rows, []string{
				posture.Name,
				posture.Publisher,
				posture.Version,
				posture.Decision,
				localizedVSCodePostureReason(posture),
			})
			if len(rows) == 12 {
				break
			}
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "extension", Min: 16, Max: 30},
			{Header: "publisher", Min: 8, Max: 16},
			{Header: "version", Min: 7, Max: 14},
			{Header: "decision", Min: 8, Max: 10},
			{Header: "reason", Min: 18, Max: 40},
		}, rows, color)
		if remaining := vscodePostureReviewCount(report.VSCode) - len(rows); remaining > 0 {
			fmt.Fprintf(w, "  ... %d more review entries\n", remaining)
		}
		printNextSteps(w, "vscode posture next steps", nextSteps)
	}
	if reviewCount := registryaudit.NPMPostureReviewCount(report.NPM); reviewCount > 0 {
		fmt.Fprintln(w, "\nnpm posture")
		rows := make([][]string, 0, len(report.NPM))
		nextSteps := []string{}
		for _, posture := range report.NPM {
			if !securityDecisionNeedsAttention(posture.Decision) {
				continue
			}
			if posture.Remediation != "" && len(nextSteps) < 6 {
				nextSteps = append(nextSteps, fmt.Sprintf("%s: %s", posture.Package, truncate(oneLine(localizedSecurityRemediation(posture.Remediation)), 120)))
			}
			rows = append(rows, []string{
				posture.Package,
				posture.Version,
				posture.Latest,
				posture.Decision,
				localizedNPMPostureReason(posture),
			})
			if len(rows) == 12 {
				break
			}
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "package", Min: 12, Max: 30},
			{Header: "version", Min: 7, Max: 14},
			{Header: "latest", Min: 7, Max: 14},
			{Header: "decision", Min: 8, Max: 10},
			{Header: "reason", Min: 18, Max: 40},
		}, rows, color)
		if remaining := reviewCount - len(rows); remaining > 0 {
			fmt.Fprintf(w, "  ... %d more review entries\n", remaining)
		}
		printNextSteps(w, "npm posture next steps", nextSteps)
	}
	if reviewCount := registryaudit.CargoPostureReviewCount(report.Cargo); reviewCount > 0 {
		fmt.Fprintln(w, "\ncargo posture")
		rows := make([][]string, 0, len(report.Cargo))
		nextSteps := []string{}
		for _, posture := range report.Cargo {
			if !securityDecisionNeedsAttention(posture.Decision) {
				continue
			}
			if posture.Remediation != "" && len(nextSteps) < 6 {
				nextSteps = append(nextSteps, fmt.Sprintf("%s: %s", posture.Crate, truncate(oneLine(localizedSecurityRemediation(posture.Remediation)), 120)))
			}
			rows = append(rows, []string{
				posture.Crate,
				posture.Version,
				posture.Latest,
				posture.Decision,
				localizedCargoPostureReason(posture),
			})
			if len(rows) == 12 {
				break
			}
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "crate", Min: 12, Max: 30},
			{Header: "version", Min: 7, Max: 14},
			{Header: "latest", Min: 7, Max: 14},
			{Header: "decision", Min: 8, Max: 10},
			{Header: "reason", Min: 18, Max: 40},
		}, rows, color)
		if remaining := reviewCount - len(rows); remaining > 0 {
			fmt.Fprintf(w, "  ... %d more review entries\n", remaining)
		}
		printNextSteps(w, "cargo posture next steps", nextSteps)
	}
	if reviewCount := registryaudit.PyPIPostureReviewCount(report.PyPI); reviewCount > 0 {
		fmt.Fprintln(w, "\npypi posture")
		rows := make([][]string, 0, len(report.PyPI))
		nextSteps := []string{}
		for _, posture := range report.PyPI {
			if !securityDecisionNeedsAttention(posture.Decision) {
				continue
			}
			if posture.Remediation != "" && len(nextSteps) < 6 {
				nextSteps = append(nextSteps, fmt.Sprintf("%s: %s", posture.Package, truncate(oneLine(localizedSecurityRemediation(posture.Remediation)), 120)))
			}
			rows = append(rows, []string{
				posture.Package,
				posture.Version,
				posture.Latest,
				posture.Decision,
				localizedPyPIPostureReason(posture),
			})
			if len(rows) == 12 {
				break
			}
		}
		textui.PrintTable(w, []textui.Column{
			{Header: "package", Min: 12, Max: 30},
			{Header: "version", Min: 7, Max: 14},
			{Header: "latest", Min: 7, Max: 14},
			{Header: "decision", Min: 8, Max: 10},
			{Header: "reason", Min: 18, Max: 40},
		}, rows, color)
		if remaining := reviewCount - len(rows); remaining > 0 {
			fmt.Fprintf(w, "  ... %d more review entries\n", remaining)
		}
		printNextSteps(w, "pypi posture next steps", nextSteps)
	}
	if len(report.Findings) == 0 {
		fmt.Fprintln(w, "\nfindings: none")
		return
	}
	fmt.Fprintln(w, "\nfindings")
	rows := make([][]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		rows = append(rows, []string{
			finding.Name,
			finding.Version,
			finding.Ecosystem,
			finding.VulnID,
			finding.Severity,
			epssSummary(finding.EPSS),
			strings.Join(finding.FixedVersions, ","),
			securityPathSummary(finding.BinaryName, finding.PathState),
			finding.Decision,
			localizedSecurityReason(securityFindingReason(finding)),
		})
	}
	textui.PrintTable(w, []textui.Column{
		{Header: "name", Min: 12, Max: 36},
		{Header: "version", Min: 7, Max: 14},
		{Header: "ecosystem", Min: 9, Max: 12},
		{Header: "vuln", Min: 12, Max: 24},
		{Header: "severity", Min: 8, Max: 18},
		{Header: "epss", Min: 5, Max: 10},
		{Header: "fixed", Min: 7, Max: 16},
		{Header: "path", Min: 7, Max: 16},
		{Header: "decision", Min: 8, Max: 10},
		{Header: "reason", Min: 12, Max: 32},
	}, rows, color)
}

func securityPathSummary(binary string, state string) string {
	switch strings.TrimSpace(state) {
	case "on-path":
		if binary != "" {
			return binary + ":on"
		}
		return "on"
	case "not-found":
		if binary != "" {
			return binary + ":not-found"
		}
		return "not-found"
	case "unknown":
		return "unknown"
	default:
		return ""
	}
}

func nativeAuditIssue(audit nativeAudit) string {
	return firstNonEmpty(audit.UnavailableReason, audit.ErrorKind)
}

func scannerEvidenceIssue(scanner scannerEvidence) string {
	return firstNonEmpty(scanner.UnavailableReason, scanner.ErrorKind)
}

func securityDecisionPriority(decision string) int {
	return securitygate.DecisionPriority(decision)
}

func boolPriority(value bool) int {
	if value {
		return 1
	}
	return 0
}

func printSecurityGateText(w io.Writer, report securityGateReport) {
	status := report.Status
	if status == "" {
		status = plan.StatusOK
	}
	fmt.Fprintf(w, "updev security gate [%s]\n", status)
	fmt.Fprintf(w, "root: %s\n", report.Root)
	if report.Provider != "" {
		fmt.Fprintf(w, "provider: %s\n", report.Provider)
	}
	if report.Policy != nil && report.Policy.Loaded {
		fmt.Fprintf(w, "policy: %s (%s)\n", report.Policy.Path, securityPolicyUseSummary(report.Policy))
	}
	if report.Error != "" {
		fmt.Fprintf(w, "error: %s\n", report.Error)
		return
	}
	if len(report.Warnings) > 0 {
		fmt.Fprintln(w, "warnings:")
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "  %s\n", warning)
		}
	}
	printSafetyTextTo(w, report.Gates)
}

func printNextSteps(w io.Writer, title string, nextSteps []string) {
	if len(nextSteps) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n", title)
	for _, nextStep := range nextSteps {
		fmt.Fprintf(w, "  %s\n", nextStep)
	}
}

func printSecurityPolicyText(w io.Writer, report securityPolicyReport) {
	status := report.Status
	if status == "" {
		status = plan.StatusOK
	}
	fmt.Fprintf(w, "updev security policy [%s]\n", status)
	if report.Path != "" {
		fmt.Fprintf(w, "path: %s\n", report.Path)
	}
	if report.Error != "" {
		fmt.Fprintf(w, "error: %s\n", report.Error)
		return
	}
	if len(report.Rules) == 0 {
		fmt.Fprintln(w, "rules: none")
		printSecurityPolicyCleanupPlan(w, report.Cleanup)
		return
	}
	if report.Summary != nil {
		fmt.Fprintf(w, "rules: %d total, %d active", report.Summary.RuleCount, report.Summary.ActiveRules)
		if report.Summary.FilteredRules > 0 && report.Summary.FilteredRules != report.Summary.RuleCount {
			fmt.Fprintf(w, ", %d shown", report.Summary.FilteredRules)
		}
		if report.Summary.ExpiredRules > 0 {
			fmt.Fprintf(w, ", %d expired", report.Summary.ExpiredRules)
		}
		if report.Summary.InvalidRules > 0 {
			fmt.Fprintf(w, ", %d invalid", report.Summary.InvalidRules)
		}
		if report.Summary.DuplicateRules > 0 {
			fmt.Fprintf(w, ", %d duplicate", report.Summary.DuplicateRules)
		}
		if report.Summary.ShadowedRules > 0 {
			fmt.Fprintf(w, ", %d shadowed", report.Summary.ShadowedRules)
		}
		if report.Summary.MissingReasons > 0 {
			fmt.Fprintf(w, ", %d missing reason", report.Summary.MissingReasons)
		}
		if report.Summary.MissingExpiries > 0 {
			fmt.Fprintf(w, ", %d missing expiry", report.Summary.MissingExpiries)
		}
		if report.Summary.BroadRules > 0 {
			fmt.Fprintf(w, ", %d broad", report.Summary.BroadRules)
		}
		fmt.Fprintln(w)
	}
	rows := make([][]string, 0, len(report.Rules))
	cleanup := []string{}
	for _, rule := range report.Rules {
		rows = append(rows, []string{
			fmt.Sprint(rule.Index),
			securityPolicyRuleLineText(rule.Line),
			rule.Provider,
			rule.Kind,
			rule.Name,
			rule.Decision,
			rule.State,
			securityPolicyRuleFlags(rule),
			rule.Expires,
			rule.Reason,
		})
		if rule.Remediation != "" {
			cleanup = append(cleanup, fmt.Sprintf("#%d: %s", rule.Index, rule.Remediation))
		}
	}
	textui.PrintTable(w, []textui.Column{
		{Header: "#", Min: 1, Max: 4},
		{Header: "line", Min: 4, Max: 6},
		{Header: "provider", Min: 8, Max: 14},
		{Header: "kind", Min: 4, Max: 10},
		{Header: "name", Min: 12, Max: 28},
		{Header: "decision", Min: 8, Max: 10},
		{Header: "state", Min: 7, Max: 9},
		{Header: "flags", Min: 5, Max: 40},
		{Header: "expires", Min: 7, Max: 10},
		{Header: "reason", Min: 12, Max: 32},
	}, rows, textui.ColorEnabled())
	if len(cleanup) > 0 {
		fmt.Fprintln(w, "\npolicy cleanup")
		for _, item := range cleanup {
			fmt.Fprintf(w, "  %s\n", item)
		}
	}
	printSecurityPolicyCleanupPlan(w, report.Cleanup)
}

func printSecurityPolicyCleanupPlan(w io.Writer, cleanup []securityPolicyCleanup) {
	if len(cleanup) == 0 {
		return
	}
	fmt.Fprintln(w, "\ncleanup plan")
	for _, action := range cleanup {
		applied := ""
		if action.Applied {
			applied = " applied"
		}
		fmt.Fprintf(w, "  #%d %s%s %s %s: %s\n", action.Index, action.Action, applied, securityPolicyCleanupTarget(action), action.Name, action.Reason)
		if action.Command != "" {
			fmt.Fprintf(w, "    command: %s\n", action.Command)
		}
	}
}

func securityPolicyCleanupTarget(action securityPolicyCleanup) string {
	parts := []string{}
	if action.Provider != "" {
		parts = append(parts, action.Provider)
	}
	if action.Kind != "" {
		parts = append(parts, action.Kind)
	}
	if len(parts) == 0 {
		return "policy"
	}
	return strings.Join(parts, "/")
}

func securityPolicyRuleLineText(line int) string {
	if line <= 0 {
		return ""
	}
	return fmt.Sprint(line)
}

func securityPolicyRuleFlags(rule securityPolicyRuleView) string {
	flags := []string{}
	if rule.MissingReason {
		flags = append(flags, "missing-reason")
	}
	if rule.MissingExpiry {
		flags = append(flags, "missing-expiry")
	}
	if rule.Broad {
		flags = append(flags, "broad")
	}
	if rule.Shadowed {
		flags = append(flags, "shadowed")
	}
	if rule.ShadowedBy > 0 {
		flags = append(flags, fmt.Sprintf("shadowed-by:%d", rule.ShadowedBy))
	}
	return strings.Join(flags, ",")
}

func osvAPIURL() string {
	return configuredEnvString(defaultOSVAPIURL, "UPDEV_OSV_API_URL")
}

func cisaKEVURL() string {
	return configuredEnvString(defaultCISAKEVURL, "UPDEV_CISA_KEV_URL")
}

func epssURL() string {
	return configuredEnvString(defaultEPSSURL, "UPDEV_EPSS_URL")
}

func epssSummary(epss *epssFinding) string {
	if epss == nil {
		return ""
	}
	return fmt.Sprintf("%.3f", epss.Score)
}
