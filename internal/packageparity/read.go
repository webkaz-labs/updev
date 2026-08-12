package packageparity

import (
	"context"
	"fmt"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/mise"
	"github.com/webkaz-labs/updev/internal/runner"
)

type Snapshot struct {
	Report     Report                     `json:"report"`
	PackageSet mise.BootstrapPackageSet   `json:"package_set"`
	Taps       []mise.BootstrapTapDesired `json:"taps"`
}

func Read(ctx context.Context, root string, brewfilePath string, commandRunner runner.Runner) (Snapshot, error) {
	brewfileItems, err := brew.DesiredFromPath(brewfilePath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read active Brewfile desired state: %w", err)
	}
	packageSet, err := mise.ReadBootstrapPackageSet(ctx, commandRunner, root)
	if err != nil {
		return Snapshot{}, err
	}
	taps, err := mise.BootstrapTapsFromSources(packageSet.Sources)
	if err != nil {
		return Snapshot{}, err
	}
	report, err := Build(root, brewfilePath, brewfileItems, packageSet, taps)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Report: report, PackageSet: packageSet, Taps: taps}, nil
}
