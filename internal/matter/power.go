package matter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func (c *Client) Power(ctx context.Context, request api.PowerRequest) (api.PowerResult, error) {
	if err := request.Validate(); err != nil {
		return api.PowerResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return api.PowerResult{}, err
	}
	if !c.controlling.CompareAndSwap(false, true) {
		return api.PowerResult{}, api.ErrBusy
	}
	defer c.controlling.Store(false)
	ctx, cancel := context.WithTimeout(ctx, request.Timeout())
	defer cancel()

	c.mu.Lock()
	if !c.ready || c.current == nil {
		c.mu.Unlock()
		return api.PowerResult{}, api.ErrUnavailable
	}
	id, err := c.resolveLocked(request.Target)
	if err != nil {
		c.mu.Unlock()
		return api.PowerResult{}, err
	}
	device := project(c.nodes[id])
	session := c.current
	c.mu.Unlock()
	if !device.Available {
		return api.PowerResult{}, api.ErrOffline
	}
	var matches []uint16
	for _, endpoint := range device.Endpoints {
		if request.Endpoint != nil && endpoint.ID != *request.Endpoint {
			continue
		}
		if slices.Contains(endpoint.Capabilities, "power."+request.Action) {
			matches = append(matches, endpoint.ID)
		}
	}
	if len(matches) == 0 {
		return api.PowerResult{}, api.ErrUnsupported
	}
	if len(matches) > 1 {
		return api.PowerResult{}, errors.Join(api.ErrAmbiguous, fmt.Errorf("compatible endpoints %v; specify --endpoint", matches))
	}
	endpoint := matches[0]
	number := json.Number(id)
	path := fmt.Sprintf("%d/%d/%d", endpoint, clusterOnOff, attributeOnOff)
	readArgs := struct {
		NodeID        json.Number `json:"node_id"`
		AttributePath string      `json:"attribute_path"`
	}{number, path}
	want := request.Action == "on"
	if request.Action == "toggle" {
		var values map[string]json.RawMessage
		if err := c.callOnSessionWithTimeout(ctx, "read_attribute", readArgs, &values, request.Timeout(), session); err != nil {
			return api.PowerResult{}, fmt.Errorf("read power before toggle: %w", err)
		}
		var current bool
		if raw, ok := values[path]; !ok || json.Unmarshal(raw, &current) != nil {
			return api.PowerResult{}, errors.New("cannot determine current power before toggle")
		}
		want = !current
	}
	args := struct {
		NodeID      json.Number    `json:"node_id"`
		EndpointID  uint16         `json:"endpoint_id"`
		ClusterID   uint32         `json:"cluster_id"`
		CommandName string         `json:"command_name"`
		Payload     map[string]any `json:"payload"`
	}{number, endpoint, clusterOnOff, request.Action, map[string]any{}}
	var acknowledgement json.RawMessage
	if err := c.callOnSessionWithTimeout(ctx, "device_command", args, &acknowledgement, request.Timeout(), session); err != nil {
		var remote remoteError
		if errors.As(err, &remote) && remote.status != nil {
			return api.PowerResult{}, errors.Join(api.ErrDeviceRejected, err)
		}
		return api.PowerResult{}, fmt.Errorf("power command was not acknowledged; outcome may be unknown: %w", err)
	}
	// A read after acknowledgement establishes state independently of the cached
	// subscription snapshot, which might predate the command.
	for {
		var values map[string]json.RawMessage
		if err := c.callOnSessionWithTimeout(ctx, "read_attribute", readArgs, &values, request.Timeout(), session); err != nil {
			return api.PowerResult{}, errors.Join(api.ErrUnconfirmed, err)
		}
		var power bool
		if raw, ok := values[path]; ok && json.Unmarshal(raw, &power) == nil && power == want {
			c.publishConfirmedAttribute(session, id, endpoint, clusterOnOff, attributeOnOff, raw)
			return api.PowerResult{NodeID: id, Endpoint: endpoint, Power: power, ConfirmedAt: time.Now().UTC()}, nil
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return api.PowerResult{}, errors.Join(api.ErrUnconfirmed, ctx.Err())
		case <-timer.C:
		}
	}
}
