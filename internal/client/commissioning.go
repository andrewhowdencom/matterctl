package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func (c *Client) Discover(ctx context.Context) (result api.Discovery, err error) {
	err = c.requestJSON(ctx, "discover-devices", http.MethodGet, api.DiscoverPath, nil, nil, &result, api.DiscoveryTimeout+2*time.Second)
	if err == nil && (result.Devices == nil || result.ObservedAt.IsZero()) {
		err = errors.New("invalid matterctl discovery response")
	}
	return result, err
}

func (c *Client) Commission(ctx context.Context, request api.CommissionRequest) (result api.Device, err error) {
	if err = request.Validate(); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	err = c.requestJSON(ctx, "commission-device", http.MethodPost, api.CommissionPath, nil, request, &result, request.Timeout()+2*time.Second)
	if err == nil && result.NodeID == "" {
		err = errors.New("invalid matterctl commissioning response")
	}
	if err != nil {
		var failure responseError
		if errors.As(err, &failure) {
			return api.Device{}, err
		}
		// A lost HTTP response cannot tell us whether commissioning completed.
		return api.Device{}, errors.Join(api.ErrOutcomeUnknown, err)
	}
	return result, nil
}
