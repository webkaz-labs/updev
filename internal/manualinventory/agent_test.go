package manualinventory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildAgentRequestPayload(t *testing.T) {
	payload, err := BuildAgentRequestPayload("manual", "enrich", []map[string]string{{"name": "Demo App"}})
	if err != nil {
		t.Fatalf("expected payload: %v", err)
	}
	var request AgentRequest
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("expected JSON payload: %v\n%s", err, string(payload))
	}
	if request.SchemaVersion != 1 || request.Provider != "manual" || request.Action != "enrich" {
		t.Fatalf("unexpected request header: %#v", request)
	}
	if request.Output == "" || len(request.Instructions) == 0 {
		t.Fatalf("expected output contract and instructions: %#v", request)
	}
	if !strings.Contains(string(payload), "review_status") {
		t.Fatalf("expected draft review status instruction:\n%s", string(payload))
	}
}
