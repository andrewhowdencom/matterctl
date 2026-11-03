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
