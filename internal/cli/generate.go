package cli

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/celest-io/remeris/internal/app"
	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/diag"
	"github.com/celest-io/remeris/internal/fsx"
)

const codeGenerateExists = name + "::generate::exists"

// generatedFileMode is the mode before the umask of the file --generate creates, as for Knope's fs::write.
const generatedFileMode = 0o666

// generate writes the default configuration, which is the one that was loaded because there is no knope.toml, to
// knope.toml in the working directory (spec F20.7).
//
// Unlike Knope, which overwrites the file whenever it could not read it, this never replaces anything: a path that
// exists in any form (a directory, a symbolic link, a file that is not UTF-8) is an error. In a dry run nothing is
// written and the file is printed instead. A package discovery error is reported before anything else, as the load
// of the configuration does in Knope.
func generate(global *app.GlobalOptions, cfg *config.Config) error {
	if err := cfg.DiscoveryError(); err != nil {
		return err
	}
	if global.DryRun {
		return generateDryRun(cfg)
	}
	slog.Info("Generating a " + config.KnopeFile + " file")
	data, err := generatedDocument(cfg)
	if err != nil {
		return err
	}
	err = fsx.WriteNew(config.KnopeFile, data, generatedFileMode)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrExist):
		return existsError()
	default:
		return diag.Newf("Could not write %s", config.KnopeFile).WithCause(err)
	}
}

func existsError() error {
	return diag.New(config.KnopeFile+" already exists").
		WithCode(codeGenerateExists).
		WithHelp("Remove or rename it to generate a new one. " + name + " never overwrites a configuration file.")
}

// generateDryRun prints the file instead of writing it. It refuses like a real run when the path is taken, since a
// dry run tells what would happen.
func generateDryRun(cfg *config.Config) error {
	if _, err := os.Lstat(config.KnopeFile); err == nil {
		return existsError()
	}
	data, err := generatedDocument(cfg)
	if err != nil {
		return err
	}
	slog.Info("Would generate a " + config.KnopeFile + " file:\n" + strings.TrimSuffix(string(data), "\n"))
	return nil
}

// generatedDocument is the TOML text of cfg. It is never empty.
func generatedDocument(cfg *config.Config) ([]byte, error) {
	doc, err := cfg.Document()
	if err != nil {
		return nil, err
	}
	data, err := doc.Encode()
	if err != nil {
		return nil, diag.Newf("Could not encode %s", config.KnopeFile).WithCause(err)
	}
	if len(data) == 0 {
		return nil, diag.New("The generated " + config.KnopeFile + " would be empty").WithCode(name + "::generate::empty")
	}
	return data, nil
}
