package reviewui

import (
	"strings"
	"time"
)

const (
	writeStatePrefix        = "write-"
	writeReasonStatePrefix  = "write-reason:"
	writeExpiryStatePrefix  = "write-expiry:"
	writeConfirmStatePrefix = "write-confirm:"
)

type WriteActionSpec struct {
	Title          string
	Prompt         string
	Description    string
	NeedsReason    bool
	NeedsExpiry    bool
	DefaultReason  string
	DefaultExpires string
}

type ExpiryValidator func(string, time.Time) (string, error)

type WriteFlow struct {
	Action       string
	Reason       string
	Expires      string
	ReturnAction string
}

func NewWriteFlow(action string, returnAction string, fallbackReturnAction string, spec WriteActionSpec) WriteFlow {
	returnAction = strings.TrimSpace(returnAction)
	if returnAction == "" {
		returnAction = fallbackReturnAction
	}
	return WriteFlow{
		Action:       action,
		Reason:       spec.DefaultReason,
		Expires:      spec.DefaultExpires,
		ReturnAction: returnAction,
	}
}

func (flow *WriteFlow) AcceptReason(value string) bool {
	if flow == nil {
		return false
	}
	reason := strings.TrimSpace(value)
	if reason == "" {
		return false
	}
	flow.Reason = reason
	return true
}

func (flow *WriteFlow) AcceptExpiry(value string, now time.Time, validate ExpiryValidator) bool {
	if flow == nil {
		return false
	}
	expires := strings.TrimSpace(value)
	if validate != nil {
		validated, err := validate(value, now)
		if err != nil {
			return false
		}
		expires = validated
	}
	if expires == "" {
		return false
	}
	flow.Expires = expires
	return true
}

func (flow WriteFlow) DefaultExpiry(now time.Time) string {
	return DefaultWriteExpiry(flow.Expires, now)
}

func (flow WriteFlow) ConfirmDescription(spec WriteActionSpec, expiresLabel string, reasonLabel string) string {
	return WriteConfirmDescription(spec.Description, flow.Expires, flow.Reason, expiresLabel, reasonLabel)
}

func IsWriteStateKey(key string) bool {
	return strings.HasPrefix(key, writeStatePrefix)
}

func IsWriteReasonStateKey(key string) bool {
	return strings.HasPrefix(key, writeReasonStatePrefix)
}

func IsWriteExpiryStateKey(key string) bool {
	return strings.HasPrefix(key, writeExpiryStatePrefix)
}

func WriteReasonStateKey(action string) string {
	return writeReasonStatePrefix + action
}

func WriteExpiryStateKey(action string) string {
	return writeExpiryStatePrefix + action
}

func WriteConfirmStateKey(action string) string {
	return writeConfirmStatePrefix + action
}

func DefaultWriteExpiry(value string, now time.Time) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return now.AddDate(0, 0, 7).Format("2006-01-02")
}

func WriteConfirmDescription(description string, expires string, reason string, expiresLabel string, reasonLabel string) string {
	description = strings.TrimSpace(description)
	if strings.TrimSpace(expires) != "" {
		description = appendLine(description, expiresLabel+expires)
	}
	if strings.TrimSpace(reason) != "" {
		description = appendLine(description, reasonLabel+reason)
	}
	return description
}

func appendLine(value string, line string) string {
	if strings.TrimSpace(value) == "" {
		return line
	}
	return value + "\n" + line
}
