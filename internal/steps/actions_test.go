package steps_test

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/celest-io/remeris/internal/steps"
	"github.com/celest-io/remeris/pkg/versioning/action"
)

const (
	fileA   = "a.txt"
	fileB   = "b.txt"
	oneLine = "one\n"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(
		slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey || a.Key == slog.LevelKey {
				return slog.Attr{}
			}
			return a
		}})),
	)
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func TestExecuteActionsDryRun(t *testing.T) {
	t.Chdir(t.TempDir())
	logs := captureLogs(t)
	if err := os.WriteFile(fileB, []byte(oneLine), 0o600); err != nil {
		t.Fatal(err)
	}
	tag := action.AddTag("v1.0.0")
	actions := []action.Action{
		action.WriteToFile(fileA, "new", "version = 1.0.0"),
		action.RemoveFile(fileB),
		tag,
	}

	rest, err := steps.ExecuteActions(true, actions)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(fileA); !os.IsNotExist(err) {
		t.Error("a dry run must not write")
	}
	if _, err := os.Stat(fileB); err != nil {
		t.Error("a dry run must not remove")
	}
	want := "msg=\"Would add the following to a.txt: version = 1.0.0\"\nmsg=\"Would delete b.txt\"\n"
	if got := logs.String(); got != want {
		t.Errorf("logs = %q, want %q", got, want)
	}
	if len(rest) != 1 || !rest[0].Equal(tag) {
		t.Errorf("remainder = %v", rest)
	}
}

func TestExecuteActionsReal(t *testing.T) {
	t.Chdir(t.TempDir())
	logs := captureLogs(t)
	if err := os.WriteFile(fileB, []byte(oneLine), 0o600); err != nil {
		t.Fatal(err)
	}
	release := action.CreateRelease(action.Release{Title: "1.0.0", Notes: "notes"})
	actions := []action.Action{
		action.WriteToFile(fileA, "no newline added", "ignored in a real run"),
		action.RemoveFile(fileB),
		action.RemoveFile("missing.txt"), // a failed removal is ignored
		release,
		action.AddTag("v1.0.0"),
	}

	rest, err := steps.ExecuteActions(false, actions)
	if err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(fileA); string(got) != "no newline added" {
		t.Errorf("content = %q", got)
	}
	if _, err := os.Stat(fileB); !os.IsNotExist(err) {
		t.Error("the file should be removed")
	}
	if logs.Len() != 0 {
		t.Errorf("a real run logs nothing, got %q", logs)
	}
	if len(rest) != 2 || !rest[0].Equal(release) || rest[1].Kind != action.KindAddTag {
		t.Errorf("remainder keeps the order of the non-file actions: %v", rest)
	}
}

func TestExecuteActionsWriteOrder(t *testing.T) {
	t.Chdir(t.TempDir())
	logs := captureLogs(t)
	actions := []action.Action{
		action.WriteToFile(fileB, "1", "d1"),
		action.WriteToFile(fileA, "2", "d2"),
		action.WriteToFile(fileB, "3", "d3"),
	}

	if _, err := steps.ExecuteActions(true, actions); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "b.txt: d1") || !strings.Contains(lines[1], "a.txt: d2") ||
		!strings.Contains(lines[2], "b.txt: d3") {
		t.Errorf("logs = %q", lines)
	}
	if _, err := steps.ExecuteActions(false, actions); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(fileB); string(got) != "3" {
		t.Errorf("the last write wins: %q", got)
	}
}

func TestExecuteActionsWriteError(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("dir", 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := steps.ExecuteActions(false, []action.Action{action.WriteToFile("dir", "x", "")})

	if err == nil || !strings.HasPrefix(err.Error(), "Error writing to dir: ") {
		t.Errorf("err = %v", err)
	}
}
