package app

import (
	"strings"

	"github.com/knadh/koanf/providers/cliflagv3"
	"github.com/knadh/koanf/v2"
	"github.com/urfave/cli/v3"
)

// LoadConfig copies the flags of cmd that were set, on the command line or through their
// environment variable, into cfg. Values that were not set stay as they are, so cfg should start
// from a Default…Options constructor.
//
// Only cmd's own flags are read, because that is all cliflagv3 can see. Load GlobalOptions from
// the root command and WorkflowOptions from the workflow command.
func LoadConfig[T any](cmd *cli.Command, cfg *T) error {
	k := koanf.New(".")
	if err := k.Load(cliflagv3.Provider(cmd, "."), nil); err != nil {
		return err
	}
	return k.UnmarshalWithConf(commandPath(cmd), cfg, koanf.UnmarshalConf{Tag: "mapstructure"})
}

// commandPath is the koanf key under which cliflagv3 stores cmd's flags: the names of the command
// and its ancestors from the root down, joined with dots ("remeris" or "remeris.release").
// readykit used cmd.Name alone, which is only right for the root command.
func commandPath(cmd *cli.Command) string {
	lineage := cmd.Lineage()
	names := make([]string, 0, len(lineage))
	for i := len(lineage) - 1; i >= 0; i-- {
		if lineage[i] != nil {
			names = append(names, lineage[i].Name)
		}
	}
	return strings.Join(names, ".")
}
