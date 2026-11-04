package api

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
)

const LightPath = "/api/v1/devices/light"

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type LightRequest struct {
	Target   string `json:"target"`
	Endpoint uint16 `json:"endpoint"`
	Action   string `json:"action"`
	Value    int64  `json:"value,omitempty"`
	Color    string `json:"color,omitempty"`
}

func (r LightRequest) Validate() error {
	if strings.TrimSpace(r.Target) == "" || r.Endpoint == 0 {
		return errors.Join(ErrInvalidRequest, errors.New("provide a target and nonzero endpoint"))
	}
	switch r.Action {
	case "brightness":
		if r.Value >= 1 && r.Value <= 100 && r.Color == "" {
			return nil
		}
	case "temperature":
		if r.Value >= 1000 && r.Value <= 10000 && r.Color == "" {
			return nil
		}
	case "color":
		if r.Value == 0 && hexColor.MatchString(r.Color) {
			return nil
		}
	}
	return errors.Join(ErrInvalidRequest, errors.New("use brightness 1–100%, temperature 1000–10000 K, or a #RRGGBB color"))
}

type LightResult struct {
	NodeID      NodeID    `json:"node_id"`
	Endpoint    uint16    `json:"endpoint"`
	Action      string    `json:"action"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

type LightSource interface {
	Light(context.Context, LightRequest) (LightResult, error)
}
