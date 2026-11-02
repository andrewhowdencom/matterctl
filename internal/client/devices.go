package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func (c *Client) Inventory(ctx context.Context) (inventory api.Inventory, err error) {
	err = c.getJSON(ctx, "list-devices", api.DevicesPath, nil, &inventory)
	if err == nil && (!inventory.Ready || inventory.Devices == nil) {
		err = errors.New("invalid inventory response")
	}
	return inventory, err
}

func (c *Client) Inspect(ctx context.Context, target string) (device api.Device, err error) {
	err = c.getJSON(ctx, "inspect-device", api.InspectPath, url.Values{"target": {target}}, &device)
	if err == nil && device.NodeID == "" {
		err = errors.New("invalid device response")
	}
	return device, err
}

// Watch bounds establishment and idle time independently of stream duration.
func (c *Client) Watch(ctx context.Context, target string, consume func(api.Event) error) (err error) {
	parent := ctx
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	ctx, span := c.tracer.Start(ctx, "watch-events", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String("http.request.method", http.MethodGet), attribute.String("http.path", api.EventsPath),
	))
	defer func() {
		if err != nil && !errors.Is(parent.Err(), context.Canceled) {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	address := c.baseURL.JoinPath(api.EventsPath)
	if target != "" {
		address.RawQuery = url.Values{"device": {target}}.Encode()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return errors.New("create event stream request")
	}
	request.Header.Set("Accept", "application/x-ndjson")
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(request.Header))
	establish := time.AfterFunc(c.timeout, func() { cancel(context.DeadlineExceeded) })
	defer establish.Stop()
	response, err := c.http.Do(request)
	establish.Stop()
	if err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return errors.New("cannot reach matterctl event stream")
	}
	defer func() { _ = response.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
	if response.StatusCode != http.StatusOK {
		var failure api.ErrorResponse
		return decodeResponse(response, &failure, 64<<10)
	}
	if response.Header.Get("Content-Type") != "application/x-ndjson" {
		return errors.New("invalid event stream content type")
	}
	idle := time.AfterFunc(c.streamIdleTimeout, func() { cancel(errors.New("event stream stalled; restart watch to resynchronize")) })
	defer idle.Stop()
	scanner := bufio.NewScanner(response.Body)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		idle.Reset(c.streamIdleTimeout)
		var event api.Event
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Type == "" || event.Data == nil {
			return errors.New("invalid event stream frame")
		}
		if event.Type == "stream_error" {
			return errors.New("event stream interrupted; restart watch to resynchronize")
		}
		if err := consume(event); err != nil {
			return err
		}
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return errors.New("event stream ended; restart watch to resynchronize")
}
