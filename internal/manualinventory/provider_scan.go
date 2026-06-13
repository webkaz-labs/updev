package manualinventory

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/webkaz-labs/updev/internal/plan"
	"github.com/webkaz-labs/updev/internal/runner"
)

func LiveCaskInventoryItems(root string, shouldUseHomeBrewfile bool, commandRunner runner.Runner) []plan.Item {
	if runtime.GOOS != "darwin" || !shouldUseHomeBrewfile {
		return nil
	}
	if _, err := commandRunner.LookPath("brew"); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "brew", "list", "--cask", "-1")
	if result.Err != nil {
		return nil
	}
	items := []plan.Item{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		items = append(items, plan.Item{
			Provider: "brew",
			Kind:     "cask",
			Name:     name,
			Status:   plan.StatusExtra,
			Live:     true,
		})
	}
	return items
}

func InstalledMASApps(root string, defaultRoot string, commandRunner runner.Runner) []MASApp {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(defaultRoot) == "" || filepath.Clean(root) != filepath.Clean(defaultRoot) {
		return nil
	}
	if _, err := commandRunner.LookPath("mas"); err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := commandRunner.Run(ctx, "mas", "list")
	if result.Code != 0 || strings.TrimSpace(result.Stdout) == "" {
		return nil
	}
	return ParseMASList(result.Stdout)
}
