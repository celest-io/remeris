package cli

import (
	"context"
	"fmt"
	"regexp"

	"github.com/urfave/cli/v3"

	"github.com/celest-io/remeris/internal/app"
	"github.com/celest-io/remeris/internal/diag"
)

// unknownFlag matches the error urfave/cli gives for a flag the command does not define.
var unknownFlag = regexp.MustCompile(`^flag provided but not defined: -{1,2}(\S+)$`)

// usageError turns the errors of the flag parser into diagnostics. An unknown flag reads like clap's, which is what
// Knope prints (for example --validate without a knope.toml, where the flag does not exist), and the usage text that
// urfave/cli prints on its own is dropped. Other errors are returned as they are.
func usageError(_ context.Context, _ *cli.Command, err error, _ bool) error {
	m := unknownFlag.FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	arg := "--" + m[1]
	if len(m[1]) == 1 {
		arg = "-" + m[1]
	}
	return diag.Newf("unexpected argument '%s' found", arg).
		WithHelp(fmt.Sprintf("To pass '%s' as a value, use '-- %[1]s'.", arg))
}

// specialModeGiven reports whether --generate, --upgrade or --validate was given.
func specialModeGiven(global *app.GlobalOptions) bool {
	return global.Generate || global.Upgrade || global.Validate
}
