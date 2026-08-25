package domain

import "time"

type AuditEvent struct {
	Sequence int64         `json:"sequence"`
	Type     string        `json:"type"`
	Actor    string        `json:"actor"`
	At       time.Time     `json:"at"`
	From     PackageStatus `json:"from,omitempty"`
	To       PackageStatus `json:"to,omitempty"`
	Detail   string        `json:"detail,omitempty"`
}
