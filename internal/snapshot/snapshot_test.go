package snapshot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAndRestoreSnapshot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	path := filepath.Join(root, "Brewfile.tmpl")
	if err := os.WriteFile(path, []byte("brew \"git\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := Create(root, []string{path})
	if err != nil {
		t.Fatal(err)
	}
	if snap.SchemaVersion != SchemaVersion || snap.Token == "" || len(snap.Files) != 1 {
		t.Fatalf("unexpected snapshot: %#v", snap)
	}
	if err := os.WriteFile(path, []byte("brew \"jq\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(root, snap.Token)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Token != snap.Token {
		t.Fatalf("expected token %s, got %#v", snap.Token, restored)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "brew \"git\"\n" {
		t.Fatalf("expected restored content, got %q", data)
	}
}

func TestLoadRejectsUnsafeToken(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := Load("../outside"); err == nil {
		t.Fatal("expected unsafe token to be rejected")
	}
}
