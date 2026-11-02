// Package client implements the CLI's HTTP connection to the matterctl service.
package client

import (
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
	baseURL *url.URL
	http    *http.Client
	tracer  trace.Tracer
	timeout time.Duration
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
		baseURL: u,
		http:    &http.Client{},
		timeout: 5 * time.Second,
		tracer:  otel.Tracer("github.com/andrewhowdencom/matterctl/internal/client"),
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
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	ctx, span := c.tracer.Start(ctx, "server-info", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String("http.request.method", http.MethodGet), attribute.String("http.path", api.ServerInfoPath),
	))
	defer func() {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	address := c.baseURL.JoinPath(api.ServerInfoPath)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return status, errors.New("create server information request")
	}
	request.Header.Set("Accept", "application/json")
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(request.Header))
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return status, ctx.Err()
		}
		return status, errors.New("cannot reach matterctl service; check that serve is running and server.url is correct")
	}
	defer func() { _ = response.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
	decoder := json.NewDecoder(io.LimitReader(response.Body, 64<<10))
	if err := decoder.Decode(&status); err != nil {
		return status, fmt.Errorf("invalid matterctl response (HTTP %d)", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return status, fmt.Errorf("matterctl server information request failed (HTTP %d)", response.StatusCode)
	}
	if status.Connection.State != "connected" || status.Server == nil {
		return status, errors.New("invalid matterctl response: missing connected server information")
	}
	return status, nil
}
