package brew

import (
	"strings"

	"github.com/webkaz-labs/updev/internal/githubrepo"
	"github.com/webkaz-labs/updev/internal/securityadvisory"
)

type AdvisoryMapping struct {
	Ecosystem string
	Package   string
}

var curatedAdvisoryMappings = map[string]AdvisoryMapping{
	"brew:pnpm":        {Ecosystem: "npm", Package: "pnpm"},
	"brew:yarn":        {Ecosystem: "npm", Package: "yarn"},
	"brew:typescript":  {Ecosystem: "npm", Package: "typescript"},
	"brew:mise":        {Ecosystem: "crates.io", Package: "mise"},
	"brew:ripgrep":     {Ecosystem: "crates.io", Package: "ripgrep"},
	"brew:fd":          {Ecosystem: "crates.io", Package: "fd-find"},
	"brew:cargo-audit": {Ecosystem: "crates.io", Package: "cargo-audit"},
}

func AdvisoryPackagesFromPostures(postures []Posture) []securityadvisory.Package {
	packages := []securityadvisory.Package{}
	seen := map[string]bool{}
	for _, posture := range postures {
		if posture.Kind != "brew" && posture.Kind != "cask" {
			continue
		}
		for _, pkg := range AdvisoryPackages(posture.Kind, posture.Name, posture.Version, posture.URL) {
			key := pkg.Ecosystem + "\x00" + pkg.Package + "\x00" + pkg.Version
			if seen[key] {
				continue
			}
			seen[key] = true
			packages = append(packages, pkg)
		}
	}
	return packages
}

func AdvisoryPackages(kind string, name string, version string, rawURL string) []securityadvisory.Package {
	packages := []securityadvisory.Package{}
	if rawURL != "" {
		if repo, tag, ok := githubrepo.RepoTagFromURL(rawURL); ok {
			packages = append(packages, securityadvisory.Package{
				Provider:   "brew",
				Name:       name,
				Version:    tag,
				Ecosystem:  "GIT",
				Package:    githubRepoGitURL(repo),
				Confidence: "medium",
			})
		}
	}
	if mapped, ok := CuratedAdvisoryMapping(kind, name); ok && version != "" {
		packages = append(packages, securityadvisory.Package{
			Provider:   "brew",
			Name:       name,
			Version:    version,
			Ecosystem:  mapped.Ecosystem,
			Package:    mapped.Package,
			Confidence: "medium",
		})
	}
	return packages
}

func CuratedAdvisoryMapping(kind string, name string) (AdvisoryMapping, bool) {
	mapping, ok := curatedAdvisoryMappings[strings.ToLower(kind+":"+name)]
	return mapping, ok
}

func PostureReviewCount(postures []Posture) int {
	count := 0
	for _, posture := range postures {
		if !strings.EqualFold(strings.TrimSpace(posture.Decision), "allow") {
			count++
		}
	}
	return count
}

func HasPostureReview(postures []Posture) bool {
	return PostureReviewCount(postures) > 0
}

func githubRepoGitURL(repo string) string {
	return "https://github.com/" + repo + ".git"
}
