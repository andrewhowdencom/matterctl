package api

import (
	"context"
	"errors"
	"strings"
	"time"
)

const (
	DiscoverPath         = "/api/v1/devices/discover"
	CommissionPath       = "/api/v1/devices/commission"
	DiscoveryTimeout     = 30 * time.Second
	CommissionTimeout    = 5 * time.Minute
	MaxCommissionTimeout = 10 * time.Minute
)

var (
	ErrInvalidRequest = errors.New("invalid request")
	ErrBusy           = errors.New("a commissioning request is already waiting for completion")
	ErrOutcomeUnknown = errors.New("commissioning outcome unknown; Matter Server may still be working; check device list before retrying")
)

// Commissionable contains advertised identity, not a commissioned node ID.
// Unknown upstream fields (including rotating identifiers) are not forwarded.
type Commissionable struct {
	InstanceName      string  `json:"instance_name"`
	DeviceName        string  `json:"device_name"`
	VendorID          *int    `json:"vendor_id,omitempty"`
	ProductID         *int    `json:"product_id,omitempty"`
	DeviceType        *uint32 `json:"device_type,omitempty"`
	LongDiscriminator *uint16 `json:"long_discriminator,omitempty"`
	CommissioningMode *uint8  `json:"commissioning_mode,omitempty"`
}

type Discovery struct {
	ObservedAt time.Time        `json:"observed_at"`
	Devices    []Commissionable `json:"devices"`
}

// Pairing codes are request-only: never bind them to configuration or emit them.
type CommissionRequest struct {
	Code        string `json:"code"`
	NetworkOnly bool   `json:"network_only"`
	TimeoutMS   int64  `json:"timeout_ms,omitempty"`
}

func (r CommissionRequest) Timeout() time.Duration {
	if r.TimeoutMS == 0 {
		return CommissionTimeout
	}
	return time.Duration(r.TimeoutMS) * time.Millisecond
}

func (r CommissionRequest) Validate() error {
	if !r.NetworkOnly {
		return errors.Join(ErrInvalidRequest, errors.New("only network-only commissioning is supported"))
	}
	if r.TimeoutMS != 0 && (r.TimeoutMS < 1000 || r.TimeoutMS > MaxCommissionTimeout.Milliseconds()) {
		return errors.Join(ErrInvalidRequest, errors.New("commissioning timeout must be between 1s and 10m"))
	}
	_, err := NormalizePairingCode(r.Code)
	return err
}

// NormalizePairingCode checks the input shape; Matter Server verifies the payload
// and manual-code checksum. Errors deliberately contain no submitted input.
func NormalizePairingCode(code string) (string, error) {
	invalid := func() (string, error) {
		return "", errors.Join(ErrInvalidRequest, errors.New("provide an MT: QR payload or an 11- or 21-digit manual sharing code"))
	}
	if len(code) > 1024 {
		return invalid()
	}
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "MT:") {
		if len(code) <= 3 {
			return invalid()
		}
		for _, char := range code[3:] {
			if !strings.ContainsRune("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ-.", char) {
				return invalid()
			}
		}
		return code, nil
	}
	code = strings.NewReplacer("-", "", " ", "").Replace(code)
	if len(code) != 11 && len(code) != 21 {
		return invalid()
	}
	for _, char := range code {
		if char < '0' || char > '9' {
			return invalid()
		}
	}
	return code, nil
}

type CommissioningSource interface {
	Discover(context.Context) (Discovery, error)
	Commission(context.Context, CommissionRequest) (Device, error)
}
