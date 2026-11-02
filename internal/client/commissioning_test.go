package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/andrewhowdencom/matterctl/internal/api"
)

func TestCommissionDoesNotFollowRedirectOrReplay(t *testing.T) {
	var attempts, forwarded atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Add(1) }))
	defer destination.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	c, err := New(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Commission(context.Background(), api.CommissionRequest{Code: "35325335079", NetworkOnly: true}); err == nil {
		t.Fatal("accepted a redirect")
	}
	if attempts.Load() != 1 || forwarded.Load() != 0 {
		t.Fatal("replayed or forwarded the pairing request")
	}
}
