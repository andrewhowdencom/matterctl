package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/matterctl/internal/api"
	"github.com/andrewhowdencom/matterctl/internal/client"
)

type commissioningStub struct {
	discover   func(context.Context) (api.Discovery, error)
	commission func(context.Context, api.CommissionRequest) (api.Device, error)
}

func (s commissioningStub) Discover(ctx context.Context) (api.Discovery, error) {
	if s.discover == nil {
		return api.Discovery{}, api.ErrUnavailable
	}
	return s.discover(ctx)
}
func (s commissioningStub) Commission(ctx context.Context, r api.CommissionRequest) (api.Device, error) {
	if s.commission == nil {
		return api.Device{}, api.ErrUnavailable
	}
	return s.commission(ctx, r)
}

func TestCommissionHTTPRejectsInvalidBodiesWithoutEchoingSecrets(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	called := false
	source := commissioningStub{commission: func(context.Context, api.CommissionRequest) (api.Device, error) {
		called = true
		return api.Device{}, nil
	}}
	for _, body := range []string{
		`{"code":"35325335079","network_only":false}`,
		`{"code":"secret-invalid","network_only":true}`,
		`{"code":"35325335079","network_only":true,"timeout_ms":999}`,
		`{"code":"35325335079","network_only":true,"secret-field":"secret-value"}`,
		`{"code":"35325335079","network_only":true} {}`,
		`{"code":"` + strings.Repeat("1", 5000) + `","network_only":true}`,
		`null`,
	} {
		r := httptest.NewRequest(http.MethodPost, api.CommissionPath, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler(logger, WithCommissioning(source)).ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest || called || !json.Valid(w.Body.Bytes()) || strings.Contains(w.Body.String(), "35325335079") || strings.Contains(w.Body.String(), "secret-") {
			t.Fatalf("invalid request handling: %d %s", w.Code, w.Body)
		}
	}
	if strings.Contains(logs.String(), "secret") || strings.Contains(logs.String(), "35325335079") {
		t.Fatal("request body leaked to logs")
	}
	r := httptest.NewRequest(http.MethodPost, api.CommissionPath, strings.NewReader(`{"code":"35325335079","network_only":true}`))
	w := httptest.NewRecorder()
	handler(logger, WithCommissioning(source)).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest || called {
		t.Fatal("accepted a non-JSON content type")
	}
}

func TestCommissionHTTPWaitsBeyondOrdinaryTimeouts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	source := commissioningStub{
		discover: func(ctx context.Context) (api.Discovery, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("unbounded discovery")
			}
			return api.Discovery{ObservedAt: time.Now().UTC(), Devices: []api.Commissionable{}}, nil
		},
		commission: func(ctx context.Context, request api.CommissionRequest) (api.Device, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > time.Second || !request.NetworkOnly || request.Code != "35325335079" {
				t.Error("wrong commissioning request or deadline")
			}
			timer := time.NewTimer(40 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return api.Device{}, ctx.Err()
			case <-timer.C:
			}
			return api.Device{NodeID: "18446744069414584320", Label: "Shared lamp"}, nil
		},
	}
	s := httptest.NewUnstartedServer(handler(logger, WithCommissioning(source)))
	s.Config.WriteTimeout = 10 * time.Millisecond
	s.Start()
	defer s.Close()
	c, err := client.New(s.URL, client.WithTimeout(5*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if result, err := c.Discover(context.Background()); err != nil || result.Devices == nil {
		t.Fatalf("discovery: %+v %v", result, err)
	}
	device, err := c.Commission(context.Background(), api.CommissionRequest{Code: "35325335079", NetworkOnly: true, TimeoutMS: 1000})
	if err != nil || device.NodeID != "18446744069414584320" {
		t.Fatalf("long commissioning: %+v %v", device, err)
	}
}

func TestCommissionHTTPPropagatesClientCancellation(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	source := commissioningStub{commission: func(ctx context.Context, _ api.CommissionRequest) (api.Device, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return api.Device{}, errors.Join(api.ErrOutcomeUnknown, ctx.Err())
	}}
	s := httptest.NewServer(handler(slog.New(slog.NewTextHandler(io.Discard, nil)), WithCommissioning(source)))
	defer s.Close()
	c, err := client.New(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := c.Commission(ctx, api.CommissionRequest{Code: "35325335079", NetworkOnly: true})
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request not started")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) || !errors.Is(err, api.ErrOutcomeUnknown) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("client did not stop")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("server did not stop waiting")
	}
}
