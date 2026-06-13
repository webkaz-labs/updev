package mise

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

func ProviderMetadataURL(metadata ProviderMetadataEntry) string {
	if suffix := sanitizeProviderMetadataEnvSuffix(metadata.EnvURLSuffix); suffix != "" {
		if value := strings.TrimSpace(os.Getenv("UPDEV_PROVIDER_METADATA_URL_" + suffix)); value != "" {
			return value
		}
	}
	return metadata.URL
}

func FetchVendorReleaseNoteDate(ctx context.Context, client *http.Client, metadata ProviderMetadataEntry, version string) (time.Time, error) {
	if client == nil {
		client = http.DefaultClient
	}
	url := ProviderMetadataURL(metadata)
	if strings.TrimSpace(url) == "" {
		return time.Time{}, fmt.Errorf("metadata URL is empty")
	}
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return time.Time{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return time.Time{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4*1024*1024))
	if err != nil {
		return time.Time{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return time.Time{}, fmt.Errorf("HTTP %d from vendor release notes", response.StatusCode)
	}
	date, ok := VendorReleaseNoteDateFromBody(string(body), metadata.HeadingPattern, version)
	if !ok {
		return time.Time{}, fmt.Errorf("version %s was not found in vendor release notes", version)
	}
	return date, nil
}

func VendorReleaseNoteDateFromBody(body string, pattern string, version string) (time.Time, bool) {
	version = strings.TrimSpace(version)
	if version == "" || pattern == "" {
		return time.Time{}, false
	}
	expression, err := regexp.Compile(fmt.Sprintf(pattern, regexp.QuoteMeta(version)))
	if err != nil {
		return time.Time{}, false
	}
	match := expression.FindStringSubmatch(body)
	if len(match) < 2 {
		return time.Time{}, false
	}
	parsed, err := time.Parse("2006-01-02", strings.TrimSpace(match[1]))
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func sanitizeProviderMetadataEnvSuffix(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, value)
	value = strings.Trim(value, "_")
	for strings.Contains(value, "__") {
		value = strings.ReplaceAll(value, "__", "_")
	}
	return value
}
