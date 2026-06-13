package githubrepo

import (
	"net/url"
	"strings"
)

func RepoTagFromURL(rawURL string) (string, string, bool) {
	parsed, err := url.Parse(rawURL)
	if err != nil || !strings.EqualFold(parsed.Host, "github.com") {
		return "", "", false
	}
	parts := splitPath(parsed.EscapedPath())
	if len(parts) < 4 {
		return "", "", false
	}
	owner := parts[0]
	repo := parts[1]
	switch {
	case len(parts) >= 5 && parts[2] == "releases" && parts[3] == "download":
		tag := parts[4]
		if ValidPathPart(owner) && ValidPathPart(repo) && tag != "" {
			return owner + "/" + repo, tag, true
		}
	case len(parts) >= 6 && parts[2] == "archive" && parts[3] == "refs" && parts[4] == "tags":
		tag := strings.Join(parts[5:], "/")
		tag = TrimArchiveSuffix(tag)
		if ValidPathPart(owner) && ValidPathPart(repo) && tag != "" {
			return owner + "/" + repo, tag, true
		}
	case parts[2] == "archive":
		tag := strings.Join(parts[3:], "/")
		tag = TrimArchiveSuffix(tag)
		if ValidPathPart(owner) && ValidPathPart(repo) && tag != "" {
			return owner + "/" + repo, tag, true
		}
	}
	return "", "", false
}

func RepoFromURLs(rawURLs ...string) (string, bool) {
	for _, rawURL := range rawURLs {
		repo, ok := RepoFromAnyURL(rawURL)
		if ok {
			return repo, true
		}
	}
	return "", false
}

func VersionTagCandidates(name string, version string) []string {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	tags := []string{}
	tags = appendUnique(tags, version)
	if !strings.HasPrefix(strings.ToLower(version), "v") {
		tags = appendUnique(tags, "v"+version)
	}
	name = strings.TrimSpace(name)
	if name != "" {
		tags = appendUnique(tags, name+"-"+version)
		if !strings.HasPrefix(strings.ToLower(version), "v") {
			tags = appendUnique(tags, name+"-v"+version)
		}
	}
	return tags
}

func TrimArchiveSuffix(tag string) string {
	for _, suffix := range []string{".tar.gz", ".tar.xz", ".tar.bz2", ".tgz", ".zip"} {
		tag = strings.TrimSuffix(tag, suffix)
	}
	return tag
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
