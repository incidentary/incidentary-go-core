package ingest

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/incidentary/incidentary-go-core/retry"
	"github.com/incidentary/incidentary-go-core/surface"
	"github.com/incidentary/incidentary-go-core/wirev2"
)

func fastPolicy() *retry.Policy {
	return &retry.Policy{
		MaxAttempts: 4,
		Base:        time.Microsecond,
		Factor:      2,
		MaxWait:     time.Millisecond,
		Jitter:      false,
	}
}

func makeBatch() *wirev2.Batch {
	statusCode := int32(200)
	durationNs := int64(1_000_000)
	return &wirev2.Batch{
		Resource: wirev2.Resource{ServiceName: "payment-service"},
		Agent: wirev2.Agent{
			Type:        surface.AgentTypeOTLPBridge,
			Version:     "0.0.1",
			WorkspaceID: "org_abc123",
			Surface:     surface.SurfaceProcessor,
		},
		CaptureMode: wirev2.CaptureSkeleton,
		FlushedAt:   time.Now().UnixNano(),
		Events: []wirev2.Event{{
			ID:         "550e8400-e29b-41d4-a716-446655440000",
			Kind:       "HTTP_SERVER",
			Severity:   wirev2.SeverityInfo,
			OccurredAt: time.Now().UnixNano(),
			ServiceID:  "payment-service",
			TraceID:    "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
			StatusCode: &statusCode,
			DurationNs: &durationNs,
		}},
	}
}

func TestSendBatchSetsExpectedHeaders(t *testing.T) {
	var (
		gotAuth    string
		gotCT      string
		gotVersion string
		gotSurface string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		gotVersion = r.Header.Get("X-Incidentary-Agent-Version")
		gotSurface = r.Header.Get("X-Incidentary-Surface")
		if r.URL.Path != EndpointV2Ingest {
			t.Errorf("path = %s, want %s", r.URL.Path, EndpointV2Ingest)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"accepted":1}`)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		BaseURL:      server.URL,
		APIKey:       "test-token",
		AgentVersion: "0.0.1",
		Surface:      surface.SurfaceProcessor,
		HTTPClient:   server.Client(),
		RetryPolicy:  fastPolicy(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := client.SendBatch(context.Background(), makeBatch())
	if err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if string(resp) != `{"accepted":1}` {
		t.Fatalf("response = %s", resp)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %s", gotAuth)
	}
	if gotCT != "application/json" {
		t.Fatalf("Content-Type = %s", gotCT)
	}
	if gotVersion != "0.0.1" {
		t.Fatalf("X-Incidentary-Agent-Version = %s", gotVersion)
	}
	if gotSurface != "processor" {
		t.Fatalf("X-Incidentary-Surface = %s", gotSurface)
	}
}

func TestSendBatchOmitsSurfaceHeaderWhenZero(t *testing.T) {
	var sawSurface string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawSurface = r.Header.Get("X-Incidentary-Surface")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		BaseURL:      server.URL,
		APIKey:       "k",
		AgentVersion: "0.0.1",
		HTTPClient:   server.Client(),
		RetryPolicy:  fastPolicy(),
	})
	if _, err := client.SendBatch(context.Background(), makeBatch()); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if sawSurface != "" {
		t.Fatalf("expected X-Incidentary-Surface to be unset, got %q", sawSurface)
	}
}

func TestSendBatchRetriesOn503ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":"unavailable"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"accepted":1}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		BaseURL:      server.URL,
		APIKey:       "k",
		AgentVersion: "0.0.1",
		HTTPClient:   server.Client(),
		RetryPolicy:  fastPolicy(),
	})
	resp, err := client.SendBatch(context.Background(), makeBatch())
	if err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls (503 then 200), got %d", calls.Load())
	}
	if !strings.Contains(string(resp), "accepted") {
		t.Fatalf("response = %s", resp)
	}
}

func TestSendBatchDoesNotRetryOn401(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"missing token"}`)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		BaseURL:      server.URL,
		APIKey:       "k",
		AgentVersion: "0.0.1",
		HTTPClient:   server.Client(),
		RetryPolicy:  fastPolicy(),
	})
	_, err := client.SendBatch(context.Background(), makeBatch())
	if err == nil {
		t.Fatalf("expected error")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T (%v)", err, err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", httpErr.StatusCode)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 call (no retry on 4xx), got %d", calls.Load())
	}
}

func TestSendBatchRetriesOnNetworkError(t *testing.T) {
	var calls atomic.Int32
	// Start a server, then close it so Do() fails with a transport error
	// on every attempt. Use a short-attempt policy to keep the test fast.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	addr := server.URL
	// Capture client BEFORE closing so transport keep-alive matches the test.
	httpClient := server.Client()
	server.Close()

	policy := retry.Policy{
		MaxAttempts: 3,
		Base:        time.Microsecond,
		Factor:      2,
		MaxWait:     time.Millisecond,
	}
	client, _ := NewClient(Config{
		BaseURL:      addr,
		APIKey:       "k",
		AgentVersion: "0.0.1",
		HTTPClient:   httpClient,
		RetryPolicy:  &policy,
	})
	_, err := client.SendBatch(context.Background(), makeBatch())
	if err == nil {
		t.Fatalf("expected transport error")
	}
	if calls.Load() != 0 {
		t.Fatalf("server should never see calls; got %d", calls.Load())
	}
	if !strings.Contains(err.Error(), "exhausted 3 attempts") {
		t.Fatalf("expected exhaustion message; got %v", err)
	}
}

func TestPostOTLPRouting(t *testing.T) {
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, _ := NewClient(Config{
		BaseURL:      server.URL,
		APIKey:       "k",
		AgentVersion: "0.0.1",
		HTTPClient:   server.Client(),
		RetryPolicy:  fastPolicy(),
	})
	if _, err := client.PostOTLP(context.Background(), []byte(`{}`)); err != nil {
		t.Fatalf("PostOTLP: %v", err)
	}
	if path != EndpointOTLPTraces {
		t.Fatalf("path = %s, want %s", path, EndpointOTLPTraces)
	}
}

func TestPostOTLPEmptyBody(t *testing.T) {
	client, _ := NewClient(Config{
		BaseURL:      "https://example.invalid",
		APIKey:       "k",
		AgentVersion: "0.0.1",
		RetryPolicy:  fastPolicy(),
	})
	if _, err := client.PostOTLP(context.Background(), nil); err == nil {
		t.Fatalf("expected error")
	}
}

func TestNewClientValidatesConfig(t *testing.T) {
	cases := []Config{
		{BaseURL: "", APIKey: "k", AgentVersion: "v"},
		{BaseURL: "https://x", APIKey: "", AgentVersion: "v"},
		{BaseURL: "https://x", APIKey: "k", AgentVersion: ""},
		{BaseURL: "://bad", APIKey: "k", AgentVersion: "v"},
		{BaseURL: "no-scheme", APIKey: "k", AgentVersion: "v"},
	}
	for i, cfg := range cases {
		if _, err := NewClient(cfg); err == nil {
			t.Fatalf("case %d: expected validation error", i)
		}
	}
}
