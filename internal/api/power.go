package api

import (
	"context"
	"errors"
	"strings"
	"time"
)

const PowerPath = "/api/v1/devices/power"
const PowerTimeout = 10 * time.Second

var ErrUnsupported = errors.New("device endpoint does not support the requested operation")
var ErrOffline = errors.New("device is unavailable")
var ErrDeviceRejected = errors.New("device rejected the request")
var ErrUnconfirmed = errors.New("request was acknowledged but state was not confirmed")

type PowerRequest struct {
	Target    string  `json:"target"`
	Endpoint  *uint16 `json:"endpoint,omitempty"`
	Action    string  `json:"action"`
	TimeoutMS int64   `json:"timeout_ms,omitempty"`
}

func (r PowerRequest) Validate() error {
	if strings.TrimSpace(r.Target) == "" || (r.Action != "on" && r.Action != "off") || (r.TimeoutMS != 0 && (r.TimeoutMS < 1000 || r.TimeoutMS > 30000)) {
		return errors.Join(ErrInvalidRequest, errors.New("provide a target, on/off action, and timeout from 1s to 30s"))
	}
	return nil
}

func (r PowerRequest) Timeout() time.Duration {
	if r.TimeoutMS == 0 {
		return PowerTimeout
	}
	return time.Duration(r.TimeoutMS) * time.Millisecond
}

type PowerResult struct {
	NodeID      NodeID    `json:"node_id"`
	Endpoint    uint16    `json:"endpoint"`
	Power       bool      `json:"power"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

type PowerSource interface {
	Power(context.Context, PowerRequest) (PowerResult, error)
}
