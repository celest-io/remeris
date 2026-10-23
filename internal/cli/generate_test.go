package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/diag"
)

const (
	generatingLog = "Generating a knope.toml file\n"
	existingText  = "keep me\n"
)

// defaultConfig loads the configuration of an empty working directory, as Main does when there is no knope.toml.
func defaultConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Chdir(t.TempDir())
	cfg, err := config.Load(config.KnopeFile)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestGenerateWritesTheDocument(t *testing.T) {
	cfg := defaultConfig(t)
	out, err := run(t, cfg, "--generate")
	if err != nil {
		t.Fatal(err)
	}
	if out != generatingLog {
		t.Errorf("stdout = %q", out)
	}
	got, err := os.ReadFile(config.KnopeFile)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := cfg.Document()
	if err != nil {
		t.Fatal(err)
	}
	want, err := doc.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("file = %q, want the document %q", got, want)
	}
	// What was written loads again to the same workflows.
	loaded, err := config.Load(config.KnopeFile)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source == config.SourceDefault || len(loaded.Workflows) != len(cfg.Workflows) {
		t.Errorf("loaded source %v with %d workflows, want %d", loaded.Source, len(loaded.Workflows), len(cfg.Workflows))
	}
}

func TestGenerateNeverOverwrites(t *testing.T) {
	arrange := map[string]func(t *testing.T){
		"file": func(t *testing.T) {
			if err := os.WriteFile(config.KnopeFile, []byte(existingText), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"directory": func(t *testing.T) {
			if err := os.Mkdir(config.KnopeFile, 0o750); err != nil {
				t.Fatal(err)
			}
		},
		"dangling symlink": func(t *testing.T) {
			if err := os.Symlink("nowhere", config.KnopeFile); err != nil {
				t.Skipf("no symbolic links: %v", err)
			}
		},
	}
	for name, setup := range arrange {
		t.Run(name, func(t *testing.T) {
			cfg := defaultConfig(t)
			setup(t)
			out, err := run(t, cfg, "--generate")
			var d *diag.Error
			if !errors.As(err, &d) || d.Code() != codeGenerateExists || d.Error() != "knope.toml already exists" || d.Help() == "" {
				t.Fatalf("err = %#v", err)
			}
			if out != generatingLog {
				t.Errorf("stdout = %q", out)
			}
			if name == "file" {
				if got, _ := os.ReadFile(config.KnopeFile); string(got) != existingText {
					t.Errorf("file = %q", got)
				}
			}
			if _, err := os.Stat("nowhere"); !os.IsNotExist(err) {
				t.Errorf("the symbolic link was followed: %v", err)
			}
		})
	}
}

func TestGenerateDryRunWritesNothing(t *testing.T) {
	cfg := defaultConfig(t)
	out, err := run(t, cfg, "--dry-run", "--generate")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "Would generate a knope.toml file:\n[[workflows]]\nname = \"release\"\n") ||
		!strings.HasSuffix(out, "type = \"CreateChangeFile\"\n") {
		t.Errorf("stdout = %q", out)
	}
	if _, err := os.Stat(config.KnopeFile); !os.IsNotExist(err) {
		t.Errorf("a dry run wrote the file: %v", err)
	}
}

func TestGenerateReportsTheDiscoveryErrorFirst(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("Cargo.toml", []byte("[package\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(config.KnopeFile)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DiscoveryError() == nil {
		t.Skip("the broken Cargo.toml is not a discovery error")
	}
	for _, args := range [][]string{{"--generate"}, {"--dry-run", "--generate"}} {
		out, err := run(t, cfg, args...)
		if err == nil || out != "" {
			t.Errorf("%v: out = %q, err = %v", args, out, err)
		}
		if _, statErr := os.Stat(filepath.Join(".", config.KnopeFile)); !os.IsNotExist(statErr) {
			t.Errorf("%v wrote the file: %v", args, statErr)
		}
	}
}
