package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func (c *Client) SetLabel(ctx context.Context, request api.LabelRequest) (result api.LabelResult, err error) {
	if err := request.Validate(); err != nil {
		return result, err
	}
	err = c.requestJSON(ctx, "set-device-label", http.MethodPost, api.LabelPath, nil, request, &result, request.Timeout()+2*time.Second)
	if err == nil && (result.NodeID == "" || result.Label != request.Label) {
		err = errors.New("invalid matterctl label response")
	}
	return result, err
}

// SetLabelAfterCommission allows the new node a few seconds to enter the live
// inventory. Only a missing node is retried; an uncertain write is not repeated.
func (c *Client) SetLabelAfterCommission(ctx context.Context, request api.LabelRequest) (api.LabelResult, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		result, err := c.SetLabel(ctx, request)
		var failure responseError
		if err == nil || !errors.As(err, &failure) || failure.code != http.StatusNotFound || time.Now().After(deadline) {
			return result, err
		}
		select {
		case <-ctx.Done():
			return api.LabelResult{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}
