package server

import (
	"encoding/json"
	"errors"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func controlOutcome(err error) (outcome, reason string) {
	switch {
	case err == nil:
		return "confirmed", "confirmed by reported Matter state"
	case errors.Is(err, api.ErrUnconfirmed):
		return "unknown", "state not confirmed"
	case errors.Is(err, api.ErrOffline):
		return "failed", "device offline"
	case errors.Is(err, api.ErrUnsupported):
		return "failed", "operation unsupported"
	case errors.Is(err, api.ErrUnavailable):
		return "failed", "Matter Server unavailable"
	case errors.Is(err, api.ErrDeviceRejected):
		return "failed", "device rejected command"
	default:
		return "unknown", "request outcome unknown"
	}
}

func safeControlDetails(target, action string) json.RawMessage {
	data, _ := json.Marshal(struct {
		Target string `json:"target"`
		Action string `json:"action"`
	}{target, action})
	return data
}
