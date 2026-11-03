package api

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const LabelPath = "/api/v1/devices/label"
const LabelTimeout = 10 * time.Second

type LabelRequest struct {
	Target    string  `json:"target"`
	Endpoint  *uint16 `json:"endpoint,omitempty"`
	Label     string  `json:"label"`
	TimeoutMS int64   `json:"timeout_ms,omitempty"`
}

func (r LabelRequest) Validate() error {
	if strings.TrimSpace(r.Target) == "" || !utf8.ValidString(r.Label) || len(r.Label) > 32 ||
		strings.TrimSpace(r.Label) != r.Label ||
		strings.IndexFunc(r.Label, unicode.IsControl) >= 0 ||
		(r.TimeoutMS != 0 && (r.TimeoutMS < 1000 || r.TimeoutMS > 30000)) {
		return errors.Join(ErrInvalidRequest, errors.New("provide a target, a label of at most 32 UTF-8 bytes without surrounding whitespace or control characters, and a timeout from 1s to 30s"))
	}
	return nil
}

func (r LabelRequest) Timeout() time.Duration {
	if r.TimeoutMS == 0 {
		return LabelTimeout
	}
	return time.Duration(r.TimeoutMS) * time.Millisecond
}

type LabelResult struct {
	NodeID      NodeID    `json:"node_id"`
	Endpoint    uint16    `json:"endpoint"`
	Label       string    `json:"label"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

type LabelSource interface {
	SetLabel(context.Context, LabelRequest) (LabelResult, error)
}
