package client

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func (c *Client) Power(ctx context.Context, request api.PowerRequest) (result api.PowerResult, err error) {
	if err := request.Validate(); err != nil {
		return result, err
	}
	err = c.requestJSON(ctx, "power-device", http.MethodPost, api.PowerPath, nil, request, &result, request.Timeout()+2*time.Second)
	if err == nil && (result.NodeID == "" || result.Power != (request.Action == "on")) {
		err = errors.New("invalid matterctl power response")
	}
	return result, err
}
