package project

import (
	"context"
	"testing"

	"github.com/celest-io/remeris/internal/config"
	"github.com/celest-io/remeris/internal/forge/jira"
)

func TestLoadBuildsTheJiraForge(t *testing.T) {
	t.Chdir(t.TempDir())
	withJira, err := Load(context.Background(), &config.Config{
		Jira: &config.Jira{URL: "https://jira.example", Project: "KEY"},
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := withJira.Jira.Config(), (jira.Config{URL: "https://jira.example", Project: "KEY"}); got != want {
		t.Errorf("config = %+v, want %+v", got, want)
	}

	without, err := Load(context.Background(), &config.Config{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if without.Jira != nil {
		t.Errorf("Jira = %v without a [jira] table", without.Jira)
	}
}
