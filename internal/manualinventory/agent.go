package manualinventory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type AgentRequest struct {
	SchemaVersion int         `json:"schema_version"`
	Provider      string      `json:"provider"`
	Action        string      `json:"action"`
	Candidates    interface{} `json:"candidates"`
	Output        string      `json:"output"`
	Instructions  []string    `json:"instructions"`
}

func BuildAgentRequestPayload(provider string, action string, candidates interface{}) ([]byte, error) {
	request := AgentRequest{
		SchemaVersion: 1,
		Provider:      provider,
		Action:        action,
		Candidates:    candidates,
		Output:        "TOML [[manual.apps]] entries with review_status = \"draft\"",
		Instructions: []string{
			"Return only TOML. Do not include prose or markdown fences.",
			"Use one [[manual.apps]] entry per candidate.",
			"Keep review_status = \"draft\" for every entry.",
			"Use [manual.apps.identifiers] for bundle_id, mas_id, cask, path, desktop_id, package_id, or app_id when known.",
			"Use [manual.apps.provenance] source = \"agent\" and evidence = [...].",
			"Add source_url or review_url when a vendor or provider page is known.",
			"Add owner, update_owner, and provider_metadata when ownership evidence is known.",
		},
	}
	return json.MarshalIndent(request, "", "  ")
}

func RunAgentCommand(command []string, payload []byte) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("manual inventory agent command timed out")
		}
		return "", fmt.Errorf("manual inventory agent command failed: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
