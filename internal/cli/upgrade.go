package cli

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/celest-io/remeris/internal/app"
	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/fsx"
	"github.com/celest-io/remeris/internal/steps"
)

// Messages of --upgrade (spec F21, Quirk Q9).
const (
	nothingToUpgrade = "Nothing to upgrade"
	upgradeWarning   = "Warning: " + config.KnopeFile + " was regenerated: its comments and formatting were not kept."
	wouldUpgrade     = "Would upgrade " + config.KnopeFile + ":"
	codeUpgradeLink  = name + "::upgrade::symlink"
	codeUpgradeWrite = name + "::upgrade::write"

	codeUpgradeUnknown = name + "::upgrade::unknown_key"
	codeUpgradeRO      = name + "::upgrade::read_only"
)

// upgradePath is the file --upgrade rewrites. The tests point it at a temporary file.
var upgradePath = config.KnopeFile

// upgradeConfig moves ignore_conventional_commits from the PrepareRelease steps to [changes] and says whether
// anything changed.
func upgradeConfig(cfg *config.Config) bool {
	upgraded := false
	for _, w := range cfg.Workflows {
		for _, step := range w.Steps {
			if p, ok := step.(*steps.PrepareRelease); ok && p.IgnoreConventionalCommits {
				p.IgnoreConventionalCommits = false
				upgraded = true
			}
		}
	}
	if upgraded {
		if cfg.Changes == nil {
			cfg.Changes = &config.Changes{}
		}
		cfg.Changes.IgnoreConventionalCommits = true
	}
	return upgraded
}

// upgrade is --upgrade. The deferred discovery error comes first, because the whole configuration, discovered
// packages included, is written out. A dry run prints the file instead of writing it.
func upgrade(global *app.GlobalOptions, cfg *config.Config) error {
	if err := cfg.DiscoveryError(); err != nil {
		return err
	}
	if !upgradeConfig(cfg) {
		slog.Info(nothingToUpgrade)
		return nil
	}
	// The rewrite keeps only what remeris reads: refuse before the file is lost, not warn after.
	if keys := cfg.UnknownKeys(); len(keys) > 0 {
		return diag.Newf("%s has keys that --upgrade would drop: %s", config.KnopeFile, strings.Join(keys, ", ")).
			WithCode(codeUpgradeUnknown).
			WithHelp("Remove them, or edit the file by hand: --upgrade regenerates the whole file and only keeps " +
				"the settings remeris understands.")
	}
	doc, err := cfg.Document()
	if err != nil {
		return err
	}
	data, err := doc.Encode()
	if err != nil {
		return err
	}
	if global.DryRun {
		slog.Info(wouldUpgrade)
		slog.Info(strings.TrimSuffix(string(data), "\n"))
		return nil
	}
	if err := fsx.ReplaceFile(upgradePath, data); err != nil {
		return upgradeError(err)
	}
	slog.Warn(upgradeWarning)
	return nil
}

func upgradeError(err error) error {
	switch {
	case errors.Is(err, fsx.ErrSymlink):
		return diag.Newf("%s is a symbolic link, so it was not changed", config.KnopeFile).
			WithCode(codeUpgradeLink).
			WithHelp("Replace the link by the file, or edit the file it points to.")
	case errors.Is(err, fsx.ErrReadOnly):
		return diag.Newf("%s is read-only, so it was not changed", config.KnopeFile).
			WithCode(codeUpgradeRO).
			WithHelp("Make the file writable and run the command again.")
	case errors.Is(err, fsx.ErrEmpty):
		return diag.Newf("Refusing to replace %s with an empty file", config.KnopeFile).
			WithCode(codeUpgradeWrite).
			WithCause(err)
	default:
		return diag.Newf("Could not write %s", config.KnopeFile).WithCode(codeUpgradeWrite).WithCause(err)
	}
}
