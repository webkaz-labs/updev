package plan

type Status string

const (
	StatusOK          Status = "ok"
	StatusDrift       Status = "drift"
	StatusMissing     Status = "missing"
	StatusExtra       Status = "extra"
	StatusUnavailable Status = "unavailable"
	StatusError       Status = "error"
	StatusHeld        Status = "held"
	StatusBlocked     Status = "blocked"
)

type Item struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Category string `json:"category,omitempty"`
	Version  string `json:"version,omitempty"`
	Desired  bool   `json:"desired"`
	Live     bool   `json:"live"`
	Status   Status `json:"status"`
	Detail   string `json:"detail,omitempty"`
}

type ProviderSummary struct {
	Name        string `json:"name"`
	Supported   bool   `json:"supported"`
	Desired     int    `json:"desired"`
	Live        int    `json:"live"`
	Missing     int    `json:"missing"`
	Extra       int    `json:"extra"`
	Unavailable bool   `json:"unavailable,omitempty"`
	Error       string `json:"error,omitempty"`
}

type Report struct {
	Status    Status            `json:"status"`
	Root      string            `json:"root"`
	Providers []ProviderSummary `json:"providers"`
	Items     []Item            `json:"items,omitempty"`
}
