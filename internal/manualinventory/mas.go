package manualinventory

import (
	"sort"
	"strings"
)

type MASApp struct {
	ID      string
	Name    string
	Version string
}

func ParseMASList(output string) []MASApp {
	apps := []MASApp{}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		id, rest, ok := strings.Cut(line, " ")
		if !ok || strings.Trim(id, "0123456789") != "" {
			continue
		}
		rest = strings.TrimSpace(rest)
		name := rest
		version := ""
		if strings.HasSuffix(rest, ")") {
			if index := strings.LastIndex(rest, " ("); index > 0 {
				name = strings.TrimSpace(rest[:index])
				version = strings.TrimSuffix(rest[index+2:], ")")
			}
		}
		if name == "" {
			continue
		}
		apps = append(apps, MASApp{ID: id, Name: name, Version: version})
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Name != apps[j].Name {
			return apps[i].Name < apps[j].Name
		}
		return apps[i].ID < apps[j].ID
	})
	return apps
}
