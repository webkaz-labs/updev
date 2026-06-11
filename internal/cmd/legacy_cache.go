package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/updevpath"
)

const noDescription = "概要なし"

type legacyCache struct {
	en          map[string]string
	ja          map[string]string
	meta        map[string]legacyMeta
	rows        map[string]legacyRowsEntry
	brewFormula map[string]string
	brewCask    map[string]string
}

type legacyMeta struct {
	Version string `json:"version"`
	DescEN  string `json:"desc_en"`
}

type legacyRowsEntry struct {
	Rows [][]string `json:"rows"`
}

type legacyBrewState struct {
	FormulaVersions map[string]string `json:"formula_versions"`
	CaskVersions    map[string]string `json:"cask_versions"`
}

var legacyCacheStore struct {
	sync.Mutex
	byDir map[string]legacyCache
}

func loadLegacyCache() legacyCache {
	dir := updevCacheDir()
	legacyCacheStore.Lock()
	if legacyCacheStore.byDir != nil {
		if cache, ok := legacyCacheStore.byDir[dir]; ok {
			legacyCacheStore.Unlock()
			return cache
		}
	}
	legacyCacheStore.Unlock()
	cache := legacyCache{
		en:          map[string]string{},
		ja:          map[string]string{},
		meta:        map[string]legacyMeta{},
		rows:        map[string]legacyRowsEntry{},
		brewFormula: map[string]string{},
		brewCask:    map[string]string{},
	}
	loadTranslationTSV(filepath.Join(dir, "desc_ja.tsv"), cache.en, cache.ja)
	loadJSON(filepath.Join(dir, "meta.json"), &cache.meta)
	loadJSON(filepath.Join(dir, "rows_cache.json"), &cache.rows)
	var brewState legacyBrewState
	if loadJSON(filepath.Join(dir, "brew_state.json"), &brewState) {
		cache.brewFormula = brewState.FormulaVersions
		cache.brewCask = brewState.CaskVersions
	}
	legacyCacheStore.Lock()
	if legacyCacheStore.byDir == nil {
		legacyCacheStore.byDir = map[string]legacyCache{}
	}
	legacyCacheStore.byDir[dir] = cache
	legacyCacheStore.Unlock()
	return cache
}

func (cache legacyCache) translatedDescription(key string, descEN string) string {
	descEN = strings.TrimSpace(descEN)
	if descEN == "" {
		descEN = noDescription
	}
	if ja := strings.TrimSpace(cache.ja[key]); ja != "" && cache.en[key] == descEN {
		return ja
	}
	return descEN
}

func (cache legacyCache) enrichItem(item plan.Item) plan.Item {
	key := legacyKey(item.Provider, item.Kind, item.Name)
	if key == "" {
		return item
	}
	if meta, ok := cache.meta[key]; ok {
		if item.Version == "" && item.Provider != "brew" {
			item.Version = normalizedVersion(item.Kind, meta.Version)
		}
		if item.Detail == "" {
			item.Detail = cache.translatedDescription(key, meta.DescEN)
		}
	}
	if item.Provider == "brew" {
		switch item.Kind {
		case "brew":
			if item.Version == "" {
				item.Version = cache.brewFormula[item.Name]
			}
		case "cask":
			if item.Version == "" {
				item.Version = normalizedVersion(item.Kind, cache.brewCask[item.Name])
			}
		}
	}
	if item.Provider == "mise" {
		if row, ok := cache.activeMiseRow(item.Name); ok {
			if item.Version == "" {
				item.Version = row.Version
			}
			if item.Detail == "" {
				item.Detail = cache.translatedDescription(key, row.DescEN)
			}
		}
	}
	return item
}

func (cache legacyCache) translationSourceForItem(item plan.Item, key string) string {
	if meta, ok := cache.meta[key]; ok {
		return meta.DescEN
	}
	if item.Provider == "mise" {
		if row, ok := cache.activeMiseRow(item.Name); ok {
			return row.DescEN
		}
	}
	return ""
}

func (cache legacyCache) toolSections(filters listOptions) []toolSection {
	sections := cache.miseSections(filters)
	for _, name := range []string{"npm", "bun", "pnpm", "python", "uv", "rust"} {
		if filters.provider != "" && !strings.EqualFold(filters.provider, name) {
			continue
		}
		if filters.kind != "" && !strings.EqualFold(filters.kind, "tool") {
			continue
		}
		if filters.category != "" && !strings.EqualFold(filters.category, name) {
			continue
		}
		entry := cache.rows[name]
		rows := make([]toolRow, 0, len(entry.Rows))
		for _, raw := range entry.Rows {
			if len(raw) < 3 {
				continue
			}
			key := name + ":" + raw[0]
			descEN := raw[len(raw)-1]
			row := toolRow{Name: raw[0], Version: raw[1], State: "active", Detail: cache.translatedDescription(key, descEN), TranslationKey: key, TranslationSource: descEN}
			if filters.status != "" && !toolRowStatusMatches(row.State, filters.status) {
				continue
			}
			if filters.query != "" && !strings.Contains(strings.ToLower(row.Name+" "+row.Version+" "+row.State+" "+row.Detail), strings.ToLower(filters.query)) {
				continue
			}
			rows = append(rows, row)
		}
		if len(rows) > 0 {
			sections = append(sections, toolSection{Name: name, Title: toolSectionTitle(name), Rows: rows})
		}
	}
	return sections
}

type legacyMiseRow struct {
	Name    string
	Version string
	Wanted  string
	State   string
	DescEN  string
}

func (cache legacyCache) activeMiseRow(name string) (legacyMiseRow, bool) {
	for _, raw := range cache.rows["mise"].Rows {
		row, ok := parseLegacyMiseRow(raw)
		if ok && row.Name == name && row.State == "active" {
			return row, true
		}
	}
	return legacyMiseRow{}, false
}

func (cache legacyCache) miseSections(filters listOptions) []toolSection {
	if filters.provider != "" && !strings.EqualFold(filters.provider, "mise") {
		return nil
	}
	if filters.kind != "" && !strings.EqualFold(filters.kind, "tool") {
		return nil
	}
	grouped := map[string][]toolRow{}
	for _, raw := range cache.rows["mise"].Rows {
		miseRow, ok := parseLegacyMiseRow(raw)
		if !ok {
			continue
		}
		key := "mise:" + miseRow.Name
		row := toolRow{
			Name:              miseRow.Name,
			Version:           miseRow.Version,
			Wanted:            miseRow.Wanted,
			State:             miseRow.State,
			Detail:            cache.translatedDescription(key, miseRow.DescEN),
			TranslationKey:    key,
			TranslationSource: miseRow.DescEN,
		}
		if filters.status != "" && !toolRowStatusMatches(row.State, filters.status) {
			continue
		}
		if filters.query != "" && !strings.Contains(strings.ToLower(row.Name+" "+row.Version+" "+row.Wanted+" "+row.State+" "+row.Detail), strings.ToLower(filters.query)) {
			continue
		}
		category := legacyMiseCategory(miseRow.Name)
		if filters.category != "" && !strings.EqualFold(filters.category, category) {
			continue
		}
		grouped[category] = append(grouped[category], row)
	}
	sections := []toolSection{}
	for _, category := range []string{"runtime", "core", "aqua", "cargo", "github", "npm", "pipx", "vfox"} {
		rows := grouped[category]
		if len(rows) == 0 {
			continue
		}
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].State != rows[j].State {
				return rows[i].State == "active"
			}
			if rows[i].Name != rows[j].Name {
				return rows[i].Name < rows[j].Name
			}
			return rows[i].Version < rows[j].Version
		})
		sections = append(sections, toolSection{Name: "mise/" + category, Title: "mise / " + category, Rows: rows})
	}
	return sections
}

func toolRowStatusMatches(state string, filter string) bool {
	normalizedState := strings.ToLower(strings.TrimSpace(state))
	normalizedFilter := strings.ToLower(strings.TrimSpace(filter))
	switch normalizedFilter {
	case "current", "active":
		return normalizedState == "active"
	case "installed":
		return normalizedState == "active" || normalizedState == "inactive" || normalizedState == "installed" || normalizedState == "managed"
	default:
		return normalizedState == normalizedFilter
	}
}

func parseLegacyMiseRow(raw []string) (legacyMiseRow, bool) {
	if len(raw) < 6 {
		return legacyMiseRow{}, false
	}
	return legacyMiseRow{
		Name:    raw[0],
		Version: raw[1],
		Wanted:  raw[2],
		State:   raw[3],
		DescEN:  raw[len(raw)-1],
	}, true
}

func legacyMiseCategory(name string) string {
	switch {
	case name == "go" || name == "node" || name == "python" || name == "rust" || name == "uv" || name == "bun":
		return "runtime"
	case strings.HasPrefix(name, "npm:"):
		return "npm"
	case strings.HasPrefix(name, "cargo:"):
		return "cargo"
	case strings.HasPrefix(name, "github:"):
		return "github"
	case strings.HasPrefix(name, "pipx:"):
		return "pipx"
	case strings.HasPrefix(name, "aqua:"):
		return "aqua"
	case strings.HasPrefix(name, "vfox:"):
		return "vfox"
	default:
		return "core"
	}
}

func updevCacheDir() string {
	return updevpath.CacheDir()
}

func loadTranslationTSV(path string, en map[string]string, ja map[string]string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		key := parts[0]
		if len(parts) > 1 {
			en[key] = parts[1]
		}
		if len(parts) > 2 {
			ja[key] = parts[2]
		}
	}
}

func saveTranslationCache(en map[string]string, ja map[string]string) {
	dir := updevCacheDir()
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	keys := make([]string, 0, len(ja))
	for key := range ja {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"\t"+en[key]+"\t"+ja[key])
	}
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	_ = os.WriteFile(filepath.Join(dir, "desc_ja.tsv"), []byte(body), 0o600)
}

func loadJSON(path string, out any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, out) == nil
}

func legacyKey(provider string, kind string, name string) string {
	switch provider {
	case "brew":
		if kind == "brew" || kind == "cask" {
			return kind + ":" + name
		}
	case "mise":
		return "mise:" + name
	}
	return ""
}

func normalizedVersion(kind string, version string) string {
	version = strings.TrimSpace(version)
	if kind == "cask" {
		if before, _, ok := strings.Cut(version, ","); ok {
			return before
		}
	}
	return version
}

func toolSectionTitle(name string) string {
	switch name {
	case "npm":
		return "npm global"
	case "bun":
		return "bun global"
	case "pnpm":
		return "pnpm global"
	case "python":
		return "Python user"
	case "uv":
		return "uv tool"
	case "rust":
		return "Rust cargo install"
	default:
		return name
	}
}
