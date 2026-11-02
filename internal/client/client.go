// Package client implements the CLI's HTTP connection to the matterctl service.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

type Client struct {
	baseURL           *url.URL
	http              *http.Client
	tracer            trace.Tracer
	timeout           time.Duration
	streamIdleTimeout time.Duration
}

type Option func(*Client)

func WithTracer(tracer trace.Tracer) Option {
	return func(c *Client) { c.tracer = tracer }
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) { c.timeout = timeout }
}

func New(address string, options ...Option) (*Client, error) {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid server.url: require an absolute http:// or https:// URL without user information, query, or fragment")
	}
	c := &Client{
		baseURL:           u,
		http:              &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		timeout:           5 * time.Second,
		streamIdleTimeout: 45 * time.Second,
		tracer:            otel.Tracer("github.com/andrewhowdencom/matterctl/internal/client"),
	}
	for _, option := range options {
		option(c)
	}
	if c.timeout <= 0 {
		return nil, errors.New("request timeout must be positive")
	}
	return c, nil
}

func (c *Client) ServerInfo(ctx context.Context) (status api.ServerStatus, err error) {
	err = c.getJSON(ctx, "server-info", api.ServerInfoPath, nil, &status)
	if err == nil && (status.Connection.State != "connected" || status.Server == nil) {
		err = errors.New("invalid matterctl response: missing connected server information")
	}
	return status, err
}

func (c *Client) getJSON(ctx context.Context, operation, path string, query url.Values, result any) (err error) {
	return c.requestJSON(ctx, operation, http.MethodGet, path, query, nil, result, c.timeout)
}

func (c *Client) requestJSON(ctx context.Context, operation, method, path string, query url.Values, input, result any, timeout time.Duration) (err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ctx, span := c.tracer.Start(ctx, operation, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String("http.request.method", method), attribute.String("http.path", path),
	))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	address := c.baseURL.JoinPath(path)
	address.RawQuery = query.Encode()
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return errors.New("encode service request")
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, address.String(), body)
	if err != nil {
		return errors.New("create service request")
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(request.Header))
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("cannot reach matterctl service; check that serve is running and server.url is correct")
	}
	defer func() { _ = response.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
	limit := int64(16 << 20)
	if path == api.ServerInfoPath {
		limit = 64 << 10
	}
	err = decodeResponse(response, result, limit)
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

type responseError struct {
	code    int
	message string
}

func (e responseError) Error() string {
	return fmt.Sprintf("matterctl request failed (HTTP %d): %s", e.code, e.message)
}

func decodeResponse(response *http.Response, result any, limit int64) error {
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit || json.Unmarshal(body, result) != nil {
		return fmt.Errorf("invalid matterctl response (HTTP %d)", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		var failure api.ErrorResponse
		if json.Unmarshal(body, &failure) == nil && failure.Error != "" {
			return responseError{code: response.StatusCode, message: failure.Error}
		}
		return fmt.Errorf("matterctl request failed (HTTP %d)", response.StatusCode)
	}
	return nil
}
