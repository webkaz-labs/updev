package manualinventory

import "testing"

func TestParseMASList(t *testing.T) {
	apps := ParseMASList("803453959 Slack (4.45.69)\n409201541 Pages (14.4)\nnot-valid\n")
	if len(apps) != 2 {
		t.Fatalf("unexpected mas list parse: %#v", apps)
	}
	if apps[0].Name != "Pages" || apps[0].ID != "409201541" || apps[0].Version != "14.4" {
		t.Fatalf("unexpected sorted first app: %#v", apps[0])
	}
	if apps[1].Name != "Slack" || apps[1].ID != "803453959" || apps[1].Version != "4.45.69" {
		t.Fatalf("unexpected second app: %#v", apps[1])
	}
}
