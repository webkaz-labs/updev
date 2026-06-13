package updatereason

import "testing"

func TestInferMiseBumpCandidateChange(t *testing.T) {
	reason := Infer("mise bump candidate set changed before apply: planned candidate go changed from 1.26.3 to 1.26.4")
	if reason.Code != MiseBumpCandidateChangedApply {
		t.Fatalf("expected candidate changed code, got %#v", reason)
	}
	if reason.Args["detail"] != "planned candidate go changed from 1.26.3 to 1.26.4" {
		t.Fatalf("unexpected args: %#v", reason.Args)
	}
}

func TestLocalizeJapaneseStrictBrewHeld(t *testing.T) {
	reason := StrictBrewHeldReason(2)
	got := LocalizeJapanese(reason)
	want := "Homebrew metadata を更新し、確認が必要な Homebrew 候補 2件を保留しました"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
