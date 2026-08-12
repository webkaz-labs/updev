package packageapply

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/webkaz-labs/updev/internal/brew"
	"github.com/webkaz-labs/updev/internal/packageexecutor"
)

func InstallCommand(root string, item packageexecutor.Item) ([]string, error) {
	if item.Executor == packageexecutor.ExecutorUnsupported {
		return nil, fmt.Errorf("package %s has no supported executor", item.Identity)
	}
	switch item.Executor {
	case packageexecutor.ExecutorNative:
		if item.Provider != "brew" {
			return nil, fmt.Errorf("package %s has no native apply adapter", item.Identity)
		}
		command := brew.InstallCommand(item.Kind, item.Name)
		if len(command) == 0 {
			return nil, fmt.Errorf("package %s has unsupported Homebrew kind %q", item.Identity, item.Kind)
		}
		return command, nil
	case packageexecutor.ExecutorMise:
		manager := item.Manager
		name := item.ManagerPackage
		if !validPathArg(root) || !validIdentityArg(manager) || !validIdentityArg(name) {
			return nil, fmt.Errorf("package %s is missing mise root, manager, or package identity", item.Identity)
		}
		if manager == "brew-tap" {
			return nil, fmt.Errorf("package %s is mise tap source metadata, not a package apply target", item.Identity)
		}
		return []string{"mise", "bootstrap", "packages", "apply", "--yes", "--cd", root, manager + ":" + name}, nil
	default:
		return nil, fmt.Errorf("package %s has unknown executor %q", item.Identity, item.Executor)
	}
}

func validPathArg(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func validIdentityArg(value string) bool {
	return value != "" && strings.IndexFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) < 0
}
