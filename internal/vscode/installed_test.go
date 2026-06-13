package vscode

import (
	"reflect"
	"testing"
)

func TestInstalledVersionsCommand(t *testing.T) {
	got := InstalledVersionsCommand()
	want := []string{"code", "--list-extensions", "--show-versions"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("InstalledVersionsCommand = %#v, want %#v", got, want)
	}
}

func TestParseInstalledVersions(t *testing.T) {
	got := ParseInstalledVersions("Publisher.Extension@1.2.3\ninvalid\nother.tool@0.1.0\n")
	want := map[string]string{
		"publisher.extension": "1.2.3",
		"other.tool":          "0.1.0",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseInstalledVersions = %#v, want %#v", got, want)
	}
}
