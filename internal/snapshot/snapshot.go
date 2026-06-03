package snapshot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const SchemaVersion = 1

type Snapshot struct {
	SchemaVersion int       `json:"schema_version"`
	Token         string    `json:"token"`
	Root          string    `json:"root"`
	CreatedAt     time.Time `json:"created_at"`
	Files         []File    `json:"files"`
}

type File struct {
	Source   string `json:"source"`
	Snapshot string `json:"snapshot"`
	Mode     uint32 `json:"mode"`
}

func Create(root string, paths []string) (Snapshot, error) {
	token := time.Now().UTC().Format("20060102T150405.000000000Z")
	dir := filepath.Join(baseDir(), token)
	snap := Snapshot{SchemaVersion: SchemaVersion, Token: token, Root: root, CreatedAt: time.Now().UTC()}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return snap, err
	}
	for _, path := range uniqueExisting(paths) {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			rel = filepath.Base(path)
		}
		target := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return snap, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return snap, err
		}
		if err := os.WriteFile(target, data, info.Mode().Perm()); err != nil {
			return snap, err
		}
		snap.Files = append(snap.Files, File{Source: path, Snapshot: target, Mode: uint32(info.Mode().Perm())})
	}
	if len(snap.Files) == 0 {
		return snap, fmt.Errorf("no snapshot files exist")
	}
	if err := saveManifest(dir, snap); err != nil {
		return snap, err
	}
	return snap, nil
}

func Restore(root string, token string) (Snapshot, error) {
	if token == "" {
		var err error
		token, err = LatestToken()
		if err != nil {
			return Snapshot{}, err
		}
	}
	snap, err := Load(token)
	if err != nil {
		return snap, err
	}
	if root != "" && snap.Root != root {
		return snap, fmt.Errorf("snapshot root mismatch: %s", snap.Root)
	}
	for _, file := range snap.Files {
		if root != "" && !withinRoot(root, file.Source) {
			return snap, fmt.Errorf("snapshot file is outside root: %s", file.Source)
		}
		data, err := os.ReadFile(file.Snapshot)
		if err != nil {
			return snap, err
		}
		if err := os.MkdirAll(filepath.Dir(file.Source), 0o700); err != nil {
			return snap, err
		}
		if err := os.WriteFile(file.Source, data, os.FileMode(file.Mode)); err != nil {
			return snap, err
		}
	}
	return snap, nil
}

func Load(token string) (Snapshot, error) {
	if !validToken(token) {
		return Snapshot{}, fmt.Errorf("invalid snapshot token: %s", token)
	}
	data, err := os.ReadFile(filepath.Join(baseDir(), token, "snapshot.json"))
	if err != nil {
		return Snapshot{}, err
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

func LatestToken() (string, error) {
	entries, err := os.ReadDir(baseDir())
	if err != nil {
		return "", err
	}
	tokens := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			tokens = append(tokens, entry.Name())
		}
	}
	sort.Strings(tokens)
	if len(tokens) == 0 {
		return "", fmt.Errorf("no snapshots found")
	}
	return tokens[len(tokens)-1], nil
}

func baseDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "updev")
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "updev", "snapshots")
}

func saveManifest(dir string, snap Snapshot) error {
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "snapshot.json"), data, 0o600)
}

func uniqueExisting(paths []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, path := range paths {
		path = filepath.Clean(path)
		if seen[path] {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

func validToken(token string) bool {
	if token == "" || strings.Contains(token, string(filepath.Separator)) {
		return false
	}
	for _, r := range token {
		if r >= '0' && r <= '9' || r == 'T' || r == 'Z' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func withinRoot(root string, path string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return false
	}
	return rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
