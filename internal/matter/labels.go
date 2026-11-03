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

// SetLabel writes the device's own NodeLabel and confirms it with a fresh read.
// Endpoint zero uses Basic Information; a bridged endpoint uses Bridged Device
// Basic Information. Other endpoints do not have an independently writable label.
func (c *Client) SetLabel(ctx context.Context, request api.LabelRequest) (api.LabelResult, error) {
	if err := request.Validate(); err != nil {
		return api.LabelResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return api.LabelResult{}, err
	}
	if !c.labeling.CompareAndSwap(false, true) {
		return api.LabelResult{}, api.ErrBusy
	}
	defer c.labeling.Store(false)
	ctx, cancel := context.WithTimeout(ctx, request.Timeout())
	defer cancel()

	c.mu.Lock()
	if !c.ready || c.current == nil {
		c.mu.Unlock()
		return api.LabelResult{}, api.ErrUnavailable
	}
	id, err := c.resolveLocked(request.Target)
	if err != nil {
		c.mu.Unlock()
		return api.LabelResult{}, err
	}
	device := project(c.nodes[id])
	session := c.current
	c.mu.Unlock()
	if !device.Available {
		return api.LabelResult{}, api.ErrOffline
	}
	endpoint, cluster := uint16(0), uint32(40)
	if request.Endpoint != nil {
		endpoint = *request.Endpoint
		if endpoint != 0 {
			cluster = 57
		}
	}
	compatible := false
	for _, ep := range device.Endpoints {
		if ep.ID != endpoint {
			continue
		}
		for _, entry := range ep.ServerClusters {
			if entry.ID == cluster && slices.Contains(entry.Attributes, 5) {
				compatible = true
			}
		}
	}
	if !compatible {
		return api.LabelResult{}, api.ErrUnsupported
	}
	path := fmt.Sprintf("%d/%d/5", endpoint, cluster)
	args := struct {
		NodeID        json.Number `json:"node_id"`
		AttributePath string      `json:"attribute_path"`
		Value         string      `json:"value"`
	}{json.Number(id), path, request.Label}
	var statuses []struct {
		Path struct {
			EndpointID  uint16 `json:"EndpointId"`
			ClusterID   uint32 `json:"ClusterId"`
			AttributeID uint32 `json:"AttributeId"`
		} `json:"Path"`
		Status uint32 `json:"Status"`
	}
	if err := c.callOnSessionWithTimeout(ctx, "write_attribute", args, &statuses, request.Timeout(), session); err != nil {
		var remote remoteError
		if errors.As(err, &remote) && remote.status != nil {
			return api.LabelResult{}, errors.Join(api.ErrDeviceRejected, err)
		}
		return api.LabelResult{}, fmt.Errorf("label write was not acknowledged; outcome may be unknown: %w", err)
	}
	if len(statuses) != 1 || statuses[0].Path.EndpointID != endpoint || statuses[0].Path.ClusterID != cluster || statuses[0].Path.AttributeID != 5 {
		return api.LabelResult{}, errors.Join(api.ErrUnconfirmed, errors.New("invalid label write status"))
	}
	if statuses[0].Status != 0 {
		return api.LabelResult{}, errors.Join(api.ErrDeviceRejected, fmt.Errorf("device status %d", statuses[0].Status))
	}
	readArgs := struct {
		NodeID        json.Number `json:"node_id"`
		AttributePath string      `json:"attribute_path"`
	}{json.Number(id), path}
	for {
		var values map[string]json.RawMessage
		if err := c.callOnSessionWithTimeout(ctx, "read_attribute", readArgs, &values, request.Timeout(), session); err != nil {
			return api.LabelResult{}, errors.Join(api.ErrUnconfirmed, err)
		}
		var label string
		if raw, ok := values[path]; ok && json.Unmarshal(raw, &label) == nil && label == request.Label {
			// A fresh read also updates target resolution when the upstream has not
			// emitted its attribute report yet.
			c.mu.Lock()
			if c.current != session || !c.ready {
				c.mu.Unlock()
				return api.LabelResult{}, errors.Join(api.ErrUnconfirmed, api.ErrUnavailable)
			}
			data, _ := json.Marshal([]any{id, path, label})
			updateErr := c.applyEventLocked(reply{Event: "attribute_updated", Data: data}, true)
			c.mu.Unlock()
			if updateErr != nil {
				return api.LabelResult{}, errors.Join(api.ErrUnconfirmed, updateErr)
			}
			return api.LabelResult{NodeID: id, Endpoint: endpoint, Label: label, ConfirmedAt: time.Now().UTC()}, nil
		}
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return api.LabelResult{}, errors.Join(api.ErrUnconfirmed, ctx.Err())
		case <-timer.C:
		}
	}
}
