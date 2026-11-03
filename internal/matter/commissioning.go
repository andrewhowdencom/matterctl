package matter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type remoteError struct {
	code          int
	reason        string
	status        *uint32
	clusterStatus *uint32
}

func (e remoteError) Error() string {
	message := fmt.Sprintf("upstream Matter Server returned error code %d", e.code)
	if e.reason != "" {
		message += " (" + e.reason + ")"
	}
	if e.status != nil {
		message += fmt.Sprintf("; device status %d", *e.status)
		if e.clusterStatus != nil {
			message += fmt.Sprintf("; cluster status %d", *e.clusterStatus)
		}
	}
	return message
}

func safeReason(reason string) string {
	switch reason {
	case "not-discovered", "passcode-mismatch", "invalid-payload", "attestation-rejected", "timeout", "other":
		return reason
	default:
		return ""
	}
}

func (c *Client) inventoryReady() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready && c.current != nil
}

func (c *Client) Discover(ctx context.Context) (api.Discovery, error) {
	if !c.inventoryReady() {
		return api.Discovery{}, api.ErrUnavailable
	}
	var devices []api.Commissionable
	if err := c.callWithTimeout(ctx, "discover", nil, &devices, api.DiscoveryTimeout); err != nil {
		return api.Discovery{}, err
	}
	if devices == nil {
		return api.Discovery{}, errors.New("invalid Matter Server discovery response")
	}
	return api.Discovery{ObservedAt: time.Now().UTC(), Devices: devices}, nil
}

func (c *Client) Commission(ctx context.Context, request api.CommissionRequest) (api.Device, error) {
	if err := request.Validate(); err != nil {
		return api.Device{}, err
	}
	if err := ctx.Err(); err != nil {
		return api.Device{}, err
	}
	if !c.inventoryReady() {
		return api.Device{}, api.ErrUnavailable
	}
	if !c.commissioning.CompareAndSwap(false, true) {
		return api.Device{}, api.ErrBusy
	}
	defer c.commissioning.Store(false)
	code, _ := api.NormalizePairingCode(request.Code)
	args := struct {
		Code        string `json:"code"`
		NetworkOnly bool   `json:"network_only"`
	}{code, true}
	var result node
	err := c.callWithTimeout(ctx, "commission_with_code", args, &result, request.Timeout())
	if err != nil {
		var remote remoteError
		if errors.As(err, &remote) {
			hint := "commissioning failed; check device list before retrying"
			switch remote.reason {
			case "not-discovered":
				hint = "no matching device found; open a fresh sharing window and check network reachability"
			case "passcode-mismatch":
				hint = "device rejected the code; obtain a fresh sharing code"
			case "invalid-payload":
				hint = "invalid pairing payload; copy a fresh sharing code"
			case "attestation-rejected":
				hint = "device certificates were not accepted"
			case "timeout":
				hint = "commissioning timed out; check device list before retrying"
			}
			return api.Device{}, fmt.Errorf("%s: %w", hint, remote)
		}
		return api.Device{}, errors.Join(api.ErrOutcomeUnknown, err)
	}
	if result.NodeID == "" || result.Attributes == nil {
		return api.Device{}, errors.Join(api.ErrOutcomeUnknown, errors.New("invalid Matter Server commissioning response"))
	}
	result.observedAt = time.Now().UTC()
	// The subscription owns the cache. A reply can arrive after newer reports;
	// overwriting the cache here would roll those reports back.
	return project(result), nil
}
