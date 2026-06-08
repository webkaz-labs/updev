//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	e2eScreenTimeout = 30 * time.Second
	e2eExitTimeout   = 10 * time.Second
	e2ePollInterval  = 50 * time.Millisecond
)

func TestInteractivePTYSmoke(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is required for PTY smoke")
	}
	exe := buildBinary(t)

	t.Run("dashboard optimized routes", func(t *testing.T) {
		pty := startPTY(t, exe, "--dry-run", "--interactive", "--security", "off")
		pty.waitScreen(t, regexp.MustCompile(`updev update (ok|held|drift|error)`))
		pty.waitScreen(t, regexp.MustCompile(`root: .*updev-e2e-root`))
		pty.waitScreen(t, regexp.MustCompile(`security: off`))
		pty.waitScreen(t, regexp.MustCompile(`update summary:`))
		pty.waitScreen(t, regexp.MustCompile(`report: .*last-`))
		pty.waitScreen(t, regexp.MustCompile(`focused actions:`))
		pty.sendKey(t, "Home")
		pty.waitScreen(t, regexp.MustCompile(`a/1=open full report|a/1=full report を開く`))
		pty.sendKey(t, "Enter")
		pty.waitScreen(t, regexp.MustCompile(`updev full report`))
		pty.sendKey(t, "Left")
		pty.waitScreen(t, regexp.MustCompile(`review actions|確認アクション`))
		pty.waitScreen(t, regexp.MustCompile(`backend convergence|backend 整理`))
		pty.sendKey(t, "q")
		pty.waitExit(t)
	})

	t.Run("list opens installed inventory and scoped backend detail", func(t *testing.T) {
		pty := startPTY(t, exe, "list", "--interactive", "--refresh")
		pty.waitScreen(t, regexp.MustCompile(`updev installed inventory`))
		pty.waitScreen(t, regexp.MustCompile(`Tab switch view`))
		pty.sendKey(t, "Tab")
		pty.waitScreen(t, regexp.MustCompile(`updev list manual`))
		pty.sendKey(t, "Tab")
		pty.waitScreen(t, regexp.MustCompile(`updev installed inventory`))
		pty.sendKey(t, "Enter")
		pty.waitScreen(t, regexp.MustCompile(`updev installed inventory|installed inventory`))
		pty.sendLiteral(t, "/")
		pty.sendLiteral(t, "ripgrep")
		pty.sendKey(t, "Enter")
		pty.waitScreen(t, regexp.MustCompile(`filter="ripgrep"`))
		pty.waitScreen(t, regexp.MustCompile(`open backend review`))
		pty.sendLiteral(t, "a")
		pty.waitScreen(t, regexp.MustCompile(`updev backend convergence: ripgrep[\s\S]*row 1/1`))
		pty.sendKey(t, "Left")
		pty.waitScreen(t, regexp.MustCompile(`updev installed inventory[\s\S]*filter="ripgrep"`))
		pty.sendKey(t, "q")
		pty.waitExit(t)
	})

	t.Run("last security opens cached interactive detail", func(t *testing.T) {
		env := newPTYEnv(t)
		env.seedLastUpdateReport(t)

		pty := env.start(t, exe, "last", "--interactive", "--section", "security")
		pty.waitScreen(t, regexp.MustCompile(`updev security details|updev update (ok|held|drift|error)|updev last`))
		pty.sendKey(t, "q")
		pty.waitExit(t)
	})
}

type ptySession struct {
	socket string
	name   string
	env    *ptyEnv
}

type ptyEnv struct {
	home string
	bin  string
}

func buildBinary(t *testing.T) string {
	t.Helper()
	root := toolRoot(t)
	exe := filepath.Join(t.TempDir(), "updev")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", exe, ".")
	cmd.Dir = root
	cache := os.Getenv("GOCACHE")
	if cache == "" {
		cache = filepath.Join(t.TempDir(), "gocache")
	}
	cmd.Env = append(os.Environ(), "GOCACHE="+cache)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return exe
}

func startPTY(t *testing.T, exe string, args ...string) *ptySession {
	t.Helper()
	return newPTYEnv(t).start(t, exe, args...)
}

func newPTYEnv(t *testing.T) *ptyEnv {
	t.Helper()
	tmpRoot := t.TempDir()
	home := filepath.Join(tmpRoot, "home")
	bin := filepath.Join(tmpRoot, "bin")
	for _, dir := range []string{
		filepath.Join(home, ".config"),
		filepath.Join(home, ".local", "share"),
		filepath.Join(home, ".cache"),
		bin,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFakeProviderTools(t, bin)
	return &ptyEnv{home: home, bin: bin}
}

func writeFakeProviderTools(t *testing.T, bin string) {
	t.Helper()
	scripts := map[string]string{
		"brew": `#!/bin/sh
case "$*" in
  "list --formula -1") printf 'ripgrep\njq\n' ;;
  "list --cask -1") ;;
  "info --json=v2"*) printf '{"formulae":[],"casks":[]}\n' ;;
  "outdated --json=v2") printf '{"formulae":[],"casks":[]}\n' ;;
  "--version") printf 'Homebrew 9.9.9\n' ;;
  *) exit 0 ;;
esac
`,
		"mise": `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'mise 2099.1.0\n'
  exit 0
fi
if [ "$1" = "settings" ] && [ "$2" = "ls" ]; then
  printf '{}\n'
  exit 0
fi
if [ "$1" = "ls" ] && [ "$2" = "--current" ] && [ "$3" = "--json" ]; then
  printf '{"ripgrep":[{"version":"14.1.1","requested_version":"latest","installed":true,"active":true}],"node":[{"version":"24.16.0","requested_version":"lts","installed":true,"active":true}]}\n'
  exit 0
fi
if [ "$1" = "ls" ] && [ "$2" = "--json" ]; then
  case "$3" in
    ripgrep) printf '[{"version":"14.1.1","requested_version":"latest","installed":true,"active":true}]\n' ;;
    node) printf '[{"version":"24.16.0","requested_version":"lts","installed":true,"active":true}]\n' ;;
    *) printf '[]\n' ;;
  esac
  exit 0
fi
exit 0
`,
		"gh": `#!/bin/sh
exit 1
`,
	}
	for name, content := range scripts {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatalf("write fake %s: %v", name, err)
		}
	}
}

func (e *ptyEnv) start(t *testing.T, exe string, args ...string) *ptySession {
	t.Helper()
	root := sourceRoot(t)
	socket := fmt.Sprintf("updev-e2e-%d-%d", os.Getpid(), time.Now().UnixNano())
	session := "updev"
	command := shellQuote(append([]string{exe}, commandArgsWithRoot(root, args)...)...)
	pty := &ptySession{socket: socket, name: session, env: e}
	t.Cleanup(func() {
		_ = pty.tmux("kill-server").Run()
	})

	out, err := pty.tmux("new-session", "-d", "-s", session, "-x", "120", "-y", "36", e.shellEnv()+" "+command).CombinedOutput()
	if err != nil {
		t.Fatalf("tmux new-session failed: %v\n%s", err, out)
	}
	return pty
}

func (e *ptyEnv) seedLastUpdateReport(t *testing.T) {
	t.Helper()
	reportPath := filepath.Join(e.home, ".cache", "updev", "reports", "last-update.json")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
  "version": 1,
  "type": "update",
  "created_at": "2026-01-01T00:00:00Z",
  "report": {
    "status": "ok",
    "root": "/repo",
    "security": "off",
    "steps": [
      {"name": "brew", "status": "ok"},
      {"name": "mise", "status": "ok"}
    ],
    "safety": [
      {"provider": "brew", "status": "ok"}
    ],
    "inventory": {"status": "ok"}
  }
}
`
	if err := os.WriteFile(reportPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *ptyEnv) shellEnv() string {
	entries := e.processEnv()
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		key, value, _ := strings.Cut(entry, "=")
		out = append(out, key+"="+shellQuote(value))
	}
	return strings.Join(out, " ")
}

func (e *ptyEnv) processEnv() []string {
	return []string{
		"HOME=" + e.home,
		"PATH=" + e.bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		"XDG_CONFIG_HOME=" + filepath.Join(e.home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(e.home, ".local", "share"),
		"XDG_CACHE_HOME=" + filepath.Join(e.home, ".cache"),
		"TERM=xterm-256color",
		"NO_COLOR=1",
		"LC_ALL=C",
		"TZ=UTC",
		"UPDEV_LANG=en",
	}
}

func (p *ptySession) waitScreen(t *testing.T, pattern *regexp.Regexp) string {
	t.Helper()
	deadline := time.Now().Add(e2eScreenTimeout)
	var screen string
	for time.Now().Before(deadline) {
		screen = p.capture(t)
		if pattern.MatchString(screen) {
			return screen
		}
		if p.exited() {
			t.Fatalf("PTY exited before %s\n%s", pattern, screen)
		}
		time.Sleep(e2ePollInterval)
	}
	t.Fatalf("timeout waiting for %s\n%s", pattern, screen)
	return ""
}

func (p *ptySession) waitExit(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(e2eExitTimeout)
	var screen string
	for time.Now().Before(deadline) {
		if p.exited() {
			return
		}
		screen = p.capture(t)
		time.Sleep(e2ePollInterval)
	}
	t.Fatalf("PTY did not exit\n%s", screen)
}

func (p *ptySession) sendLiteral(t *testing.T, value string) {
	t.Helper()
	if out, err := p.tmux("send-keys", "-t", p.name, "-l", value).CombinedOutput(); err != nil {
		t.Fatalf("tmux send literal %q failed: %v\n%s", value, err, out)
	}
}

func (p *ptySession) sendKey(t *testing.T, value string) {
	t.Helper()
	if out, err := p.tmux("send-keys", "-t", p.name, value).CombinedOutput(); err != nil {
		t.Fatalf("tmux send key %q failed: %v\n%s", value, err, out)
	}
}

func (p *ptySession) capture(t *testing.T) string {
	t.Helper()
	out, err := p.tmux("capture-pane", "-t", p.name, "-p").CombinedOutput()
	if err != nil {
		return ""
	}
	return string(out)
}

func (p *ptySession) exited() bool {
	cmd := p.tmux("has-session", "-t", p.name)
	return cmd.Run() != nil
}

func (p *ptySession) tmux(args ...string) *exec.Cmd {
	full := append([]string{"-L", p.socket}, args...)
	return exec.Command("tmux", full...)
}

func toolRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func sourceRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "updev-e2e-root")
	if err := os.MkdirAll(filepath.Join(root, "dot_config", "mise"), 0o755); err != nil {
		t.Fatal(err)
	}
	brewfile := strings.Join([]string{
		`brew "bat"`,
		`brew "ripgrep"`,
		`brew "jq"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "Brewfile.tmpl"), []byte(brewfile), 0o644); err != nil {
		t.Fatal(err)
	}
	miseConfig := strings.Join([]string{
		"[tools]",
		`ripgrep = "latest"`,
		`node = "lts"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, "dot_config", "mise", "config.toml"), []byte(miseConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func commandArgsWithRoot(root string, args []string) []string {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if args[0] == "last" {
			return append([]string{}, args...)
		}
		out := []string{args[0], "--root", root}
		return append(out, args[1:]...)
	}
	out := []string{"--root", root}
	return append(out, args...)
}

func shellQuote(values ...string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, "'"+strings.ReplaceAll(value, "'", "'\\''")+"'")
	}
	return strings.Join(out, " ")
}
