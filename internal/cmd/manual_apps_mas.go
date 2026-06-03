package cmd

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/runner"
)

type manualMASApp struct {
	ID      string
	Name    string
	Version string
}

func manualMASListSections(root string) []toolSection {
	if filepath.Clean(root) != filepath.Clean(defaultRoot()) {
		return nil
	}
	local := runner.Local{}
	if _, err := local.LookPath("mas"); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := local.Run(ctx, "mas", "list")
	if result.Code != 0 || strings.TrimSpace(result.Stdout) == "" {
		return nil
	}
	return manualMASListSectionsFromOutput(result.Stdout)
}

func manualMASListSectionsFromOutput(output string) []toolSection {
	apps := parseManualMASList(output)
	if len(apps) == 0 {
		return nil
	}
	rows := make([]toolRow, 0, len(apps))
	for _, app := range apps {
		details := []string{"source: mas list"}
		if app.ID != "" {
			details = append(details, "mas_id: "+app.ID)
		}
		if app.Version != "" {
			details = append(details, "version: "+app.Version)
		}
		rows = append(rows, toolRow{
			Name:    app.Name,
			Version: app.Version,
			State:   "installed",
			Detail:  strings.Join(details, "; "),
		})
	}
	return []toolSection{{
		Name:  "manual/mac-app-store",
		Title: "manual / Mac App Store",
		Rows:  rows,
	}}
}

func parseManualMASList(output string) []manualMASApp {
	apps := []manualMASApp{}
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
		apps = append(apps, manualMASApp{ID: id, Name: name, Version: version})
	}
	sort.SliceStable(apps, func(i, j int) bool {
		if apps[i].Name != apps[j].Name {
			return apps[i].Name < apps[j].Name
		}
		return apps[i].ID < apps[j].ID
	})
	return apps
}
