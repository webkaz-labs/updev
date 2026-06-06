//go:build e2e

package e2e

import (
	"errors"
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
		pty.waitScreen(t, regexp.MustCompile(`report: .*last-u`))
		pty.waitScreen(t, regexp.MustCompile(`focused actions:`))
		pty.waitScreen(t, regexp.MustCompile(`a/1=open update details|a/1=更新詳細を開く`))
		pty.sendKey(t, "Enter")
		pty.waitScreen(t, regexp.MustCompile(`updev update logs`))
		pty.sendLiteral(t, "b")
		pty.waitScreen(t, regexp.MustCompile(`updev update (ok|held|drift|error)`))
		pty.sendLiteral(t, "k")
		pty.sendKey(t, "Enter")
		pty.waitScreen(t, regexp.MustCompile(`updev full report`))
		pty.sendLiteral(t, "b")
		pty.waitScreen(t, regexp.MustCompile(`updev update (ok|held|drift|error)`))

		pty.sendKey(t, "End")
		pty.sendKey(t, "Enter")
		pty.waitScreen(t, regexp.MustCompile(`updev backend convergence`))
		pty.sendLiteral(t, "a")
		pty.waitScreen(t, regexp.MustCompile(`backend convergence action|Remove Brewfile|Rewrite mise backend|Remove old mise backend`))
		pty.sendKey(t, "C-c")
		pty.waitScreen(t, regexp.MustCompile(`updev update (ok|held|drift|error)`))
		pty.sendLiteral(t, "q")
		pty.waitExit(t)
	})

	t.Run("list opens installed inventory and scoped backend detail", func(t *testing.T) {
		pty := startPTY(t, exe, "list", "--interactive", "--refresh")
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
		pty.sendLiteral(t, "b")
		pty.waitScreen(t, regexp.MustCompile(`updev installed inventory[\s\S]*filter="ripgrep"`))
		pty.sendLiteral(t, "q")
		pty.waitExit(t)
	})

	t.Run("last security opens cached interactive detail", func(t *testing.T) {
		env := newPTYEnv(t)
		env.runNonTTY(t, exe, "--dry-run", "--security", "off")

		pty := env.start(t, exe, "last", "--interactive", "--section", "security")
		pty.waitScreen(t, regexp.MustCompile(`updev security details|updev update (ok|held|drift|error)|updev last`))
		pty.sendLiteral(t, "q")
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
	for _, dir := range []string{
		filepath.Join(home, ".config"),
		filepath.Join(home, ".local", "share"),
		filepath.Join(home, ".cache"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &ptyEnv{home: home}
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

func (e *ptyEnv) runNonTTY(t *testing.T, exe string, args ...string) {
	t.Helper()
	root := sourceRoot(t)
	cmd := exec.Command(exe, commandArgsWithRoot(root, args)...)
	cmd.Dir = toolRoot(t)
	cmd.Env = append(os.Environ(), e.processEnv()...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
		return
	}
	t.Fatalf("seed command failed: %v\n%s", err, out)
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
	deadline := time.Now().Add(60 * time.Second)
	var screen string
	for time.Now().Before(deadline) {
		screen = p.capture(t)
		if pattern.MatchString(screen) {
			return screen
		}
		if p.exited() {
			t.Fatalf("PTY exited before %s\n%s", pattern, screen)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s\n%s", pattern, screen)
	return ""
}

func (p *ptySession) waitExit(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var screen string
	for time.Now().Before(deadline) {
		if p.exited() {
			return
		}
		screen = p.capture(t)
		time.Sleep(200 * time.Millisecond)
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
	out, err := p.tmux("capture-pane", "-t", p.name, "-p", "-S", "-100").CombinedOutput()
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
