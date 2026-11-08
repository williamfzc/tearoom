package collab

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeHCCommand(t *testing.T, script string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "hc")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path, filepath.Join(dir, "args")
}

func TestHCCLIUnknownStatusIsNotIdle(t *testing.T) {
	path, _ := fakeHCCommand(t, `
if [ "$1" = "ls" ]; then printf '{"terminals":[{"name":"panel","state":"live"}]}'
elif [ "$1" = "get" ]; then printf '{"name":"panel","state":"live","status":null,"reported":false}'
else exit 2; fi
`)
	hc := HCCLI{Binary: path}
	got, err := hc.PanelStatus(context.Background(), "panel")
	if err != nil {
		t.Fatal(err)
	}
	if got != PanelUnknown {
		t.Fatalf("status = %s", got)
	}
}

func TestHCCLIMapsDoneSeparatelyFromIdle(t *testing.T) {
	path, _ := fakeHCCommand(t, `
if [ "$1" = "ls" ]; then printf '{"terminals":[{"name":"panel","state":"live"}]}'
elif [ "$1" = "get" ]; then printf '{"name":"panel","state":"live","status":{"state":"done"},"reported":true}'
else exit 2; fi
`)
	got, err := (HCCLI{Binary: path}).PanelStatus(context.Background(), "panel")
	if err != nil {
		t.Fatal(err)
	}
	if got != PanelDone {
		t.Fatalf("status = %s, want done", got)
	}
}

func TestHCCLISendsToDoneWithoutIfButNeverForce(t *testing.T) {
	path, argsPath := fakeHCCommand(t, `
printf '%s\n' "$*" >> "$HC_TEST_ARGS"
if [ "$1" = "ls" ]; then printf '{"terminals":[{"name":"panel","state":"live"}]}'
elif [ "$1" = "get" ]; then printf '{"name":"panel","state":"live","status":{"state":"done"},"reported":true}'
elif [ "$1" = "send" ]; then printf '{"started":true}'
else exit 2; fi
`)
	t.Setenv("HC_TEST_ARGS", argsPath)
	result, err := (HCCLI{Binary: path}).Submit(context.Background(), "panel", "next task", PanelDone)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.Started == nil || !*result.Started {
		t.Fatalf("result = %+v", result)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(args)), "\n")
	if got := lines[len(lines)-1]; got != "send --json panel next task" {
		t.Fatalf("send argv = %q, want no --if and no --force", got)
	}
	if strings.Contains(lines[len(lines)-1], "--force") || strings.Contains(lines[len(lines)-1], "--if") {
		t.Fatalf("unsafe send argv: %q", lines[len(lines)-1])
	}
}

func TestHCCLIRechecksDoneBeforeSendAndWaitsIfBlocked(t *testing.T) {
	path, argsPath := fakeHCCommand(t, `
printf '%s\n' "$*" >> "$HC_TEST_ARGS"
if [ "$1" = "ls" ]; then printf '{"terminals":[{"name":"panel","state":"live"}]}'
elif [ "$1" = "get" ]; then printf '{"name":"panel","state":"live","status":{"state":"blocked"},"reported":true}'
elif [ "$1" = "send" ]; then exit 8
else exit 2; fi
`)
	t.Setenv("HC_TEST_ARGS", argsPath)
	result, err := (HCCLI{Binary: path}).Submit(context.Background(), "panel", "next task", PanelDone)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pending || result.Accepted {
		t.Fatalf("result = %+v, want pending", result)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "send ") {
		t.Fatalf("sent after preflight observed blocked state: %q", args)
	}
}

func TestHCCLITemplateChecksFirstMessageSent(t *testing.T) {
	path, argsPath := fakeHCCommand(t, `
printf '%s\n' "$*" >> "$HC_TEST_ARGS"
if [ "$1" = "templates" ]; then printf '{"templates":[{"name":"reviewer","command":"codex --no-daemon"}]}'
else printf '{"name":"panel","first_message":{"sent":true,"started":null}}'; fi
`)
	t.Setenv("HC_TEST_ARGS", argsPath)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	templateDir := filepath.Join(config, "herdr-core", "templates")
	if err := os.MkdirAll(templateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "reviewer.md"), []byte("---\ncommand: codex --no-daemon\n---\nReview safely."), 0o600); err != nil {
		t.Fatal(err)
	}
	hc := HCCLI{Binary: path}
	result, err := hc.EnsurePanel(context.Background(), PanelRequest{
		Name: "panel", Room: "project", Role: "reviewer", AgentType: "codex", Template: "reviewer",
		Directory: t.TempDir(), Task: "Read a Room record.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || result.Started != nil {
		t.Fatalf("result = %+v", result)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"run --bg --json --no-parent --name panel", "--label tearoom.room=project", "reviewer", "Read a Room record."} {
		if !strings.Contains(string(args), want) {
			t.Fatalf("hc argv %q missing %q", args, want)
		}
	}
}

func TestHCCLIRejectsQuietTemplateBeforeLaunch(t *testing.T) {
	path, argsPath := fakeHCCommand(t, `
if [ "$1" = "templates" ]; then printf '{"templates":[{"name":"quiet","command":"claude"}]}'
else printf '%s\n' "$*" >> "$HC_TEST_ARGS"; printf '{"name":"panel","first_message":{"sent":true}}'; fi
`)
	t.Setenv("HC_TEST_ARGS", argsPath)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	dir := filepath.Join(config, "herdr-core", "templates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "quiet.md"), []byte("---\ncommand: claude\nready: quiet\n---\nstartup prompt"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (HCCLI{Binary: path}).EnsurePanel(context.Background(), PanelRequest{
		Name: "panel", Template: "quiet", Directory: t.TempDir(), Task: "do work",
	})
	if err == nil || !strings.Contains(err.Error(), "ready: quiet") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(argsPath); !os.IsNotExist(err) {
		t.Fatalf("hc run was invoked before readiness refusal: stat err=%v", err)
	}
}

func TestHCCLIRequiresCodexNoDaemonForTemplate(t *testing.T) {
	path, _ := fakeHCCommand(t, `
if [ "$1" = "templates" ]; then printf '{"templates":[{"name":"builder","command":"codex"}]}'
else exit 3; fi
`)
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	dir := filepath.Join(config, "herdr-core", "templates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "builder.md"), []byte("---\ncommand: codex\n---\nstartup"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := (HCCLI{Binary: path}).EnsurePanel(context.Background(), PanelRequest{
		Name: "panel", AgentType: "codex", Template: "builder", Directory: t.TempDir(), Task: "do work",
	})
	if err == nil || !strings.Contains(err.Error(), "--no-daemon") {
		t.Fatalf("error = %v", err)
	}
}

func TestHCConfigHomeUsesHerdrDefaultOnMacAndUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hc uses APPDATA on Windows")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	got, err := hcConfigHome()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config"); got != want {
		t.Fatalf("config home = %q, want %q", got, want)
	}
}

func TestHCCLIRequiresCodexNoDaemonForDirectCommand(t *testing.T) {
	path, _ := fakeHCCommand(t, `exit 9`)
	_, err := (HCCLI{Binary: path}).EnsurePanel(context.Background(), PanelRequest{
		Name: "panel", AgentType: "codex", Command: []string{"codex"},
		Directory: t.TempDir(), Task: "do work",
	})
	if err == nil || !strings.Contains(err.Error(), "--no-daemon") {
		t.Fatalf("error = %v", err)
	}
}
