package manualinventory

import "testing"

func TestAppKeyPrefersStableIdentity(t *testing.T) {
	if got := AppKey(App{Name: "Demo", BundleID: "COM.Example.Demo", Path: "/Applications/Demo.app"}); got != "bundle:com.example.demo" {
		t.Fatalf("expected bundle key, got %q", got)
	}
	if got := AppKey(App{Name: "Demo", IdentifierKey: "bundle_id", Identifier: "COM.Example.Demo", BundleID: "com.example.other"}); got != "bundle:com.example.demo" {
		t.Fatalf("expected compatible bundle identifier key, got %q", got)
	}
	if got := AppKey(App{Name: "Demo", IdentifierKey: "package_id", Identifier: "com.example.Demo"}); got != "package_id:com.example.demo" {
		t.Fatalf("expected generic package identifier key, got %q", got)
	}
	if got := AppKey(App{Name: "Demo", IdentifierKey: "path", Identifier: "/Tmp/CaseSensitive.AppImage"}); got != "path:/Tmp/CaseSensitive.AppImage" {
		t.Fatalf("expected clean case-preserving path identifier key, got %q", got)
	}
	if got := AppKey(App{Name: "Demo", Path: "/Applications/../Applications/Demo.app"}); got != "path:/Applications/Demo.app" {
		t.Fatalf("expected clean path key, got %q", got)
	}
	if got := AppKey(App{Name: "Demo App.app (Vendor)"}); got != "name:demoappappvendor" {
		t.Fatalf("expected normalized name key, got %q", got)
	}
}
