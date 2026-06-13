package manualinventory

import "testing"

func TestAppKeyPrefersStableIdentity(t *testing.T) {
	if got := AppKey(App{Name: "Demo", BundleID: "COM.Example.Demo", Path: "/Applications/Demo.app"}); got != "bundle:com.example.demo" {
		t.Fatalf("expected bundle key, got %q", got)
	}
	if got := AppKey(App{Name: "Demo", Path: "/Applications/../Applications/Demo.app"}); got != "path:/Applications/Demo.app" {
		t.Fatalf("expected clean path key, got %q", got)
	}
	if got := AppKey(App{Name: "Demo App.app (Vendor)"}); got != "name:demoappappvendor" {
		t.Fatalf("expected normalized name key, got %q", got)
	}
}
