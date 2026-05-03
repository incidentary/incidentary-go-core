// Package ingest provides a thin HTTP client that sends Incidentary v2
// batches with auth headers and policy-driven retries.
//
// The client deliberately stays small: it owns header construction, retry
// classification (5xx + network -> retry, 4xx -> fail fast), and request
// shaping. Encoding stays in wirev2; backoff stays in retry.
package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/incidentary/incidentary-go-core/retry"
	"github.com/incidentary/incidentary-go-core/surface"
	"github.com/incidentary/incidentary-go-core/wirev2"
)

// EndpointV2Ingest is the canonical V2 ingest path.
const EndpointV2Ingest = "/api/v2/ingest"

// EndpointOTLPTraces is the path the OTel Bridge uses to forward raw OTLP.
const EndpointOTLPTraces = "/api/v1/otlp/v1/traces"

// Config holds the inputs to NewClient.
type Config struct {
	BaseURL      string               // e.g., https://ingest.incidentary.com
	APIKey       string               // workspace token; required
	AgentVersion string               // semver of the agent; required
	Surface      surface.AgentSurface // optional; emits X-Incidentary-Surface when set
	HTTPClient   *http.Client         // optional; defaults to a 30s-timeout client
	RetryPolicy  *retry.Policy        // optional; defaults to retry.Default()
	UserAgent    string               // optional override for User-Agent header
}

// Client posts batches to an Incidentary ingest endpoint.
type Client struct {
	baseURL      *url.URL
	apiKey       string
	agentVersion string
	surface      surface.AgentSurface
	http         *http.Client
	retryPolicy  retry.Policy
	userAgent    string
}

// NewClient validates the config and returns a usable Client.
func NewClient(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("ingest: BaseURL is required")
	}
	if cfg.APIKey == "" {
		return nil, errors.New("ingest: APIKey is required")
	}
	if cfg.AgentVersion == "" {
		return nil, errors.New("ingest: AgentVersion is required")
	}
	parsed, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("ingest: invalid BaseURL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("ingest: BaseURL must be absolute (got %q)", cfg.BaseURL)
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	policy := retry.Default()
	if cfg.RetryPolicy != nil {
		policy = *cfg.RetryPolicy
	}
	ua := cfg.UserAgent
	if ua == "" {
		ua = "incidentary-go-core/0.0.1"
	}
	return &Client{
		baseURL:      parsed,
		apiKey:       cfg.APIKey,
		agentVersion: cfg.AgentVersion,
		surface:      cfg.Surface,
		http:         httpClient,
		retryPolicy:  policy,
		userAgent:    ua,
	}, nil
}

// SendBatch encodes the wirev2 Batch and POSTs it to /api/v2/ingest with
// retry semantics. Returns the server response body on the final success or
// the last error otherwise.
func (c *Client) SendBatch(ctx context.Context, batch *wirev2.Batch) ([]byte, error) {
	body, err := wirev2.Encode(batch)
	if err != nil {
		return nil, err
	}
	return c.postWithRetry(ctx, EndpointV2Ingest, "application/json", body)
}

// PostOTLP sends a raw OTLP/JSON-serialized payload to the OTLP traces
// endpoint that the Bridge uses for forwarding. Caller owns serialization.
func (c *Client) PostOTLP(ctx context.Context, body []byte) ([]byte, error) {
	if len(body) == 0 {
		return nil, errors.New("ingest: empty OTLP body")
	}
	return c.postWithRetry(ctx, EndpointOTLPTraces, "application/json", body)
}

// postWithRetry runs the HTTP POST under the configured retry policy. 4xx
// responses are returned immediately; 5xx and transport errors are retried.
func (c *Client) postWithRetry(ctx context.Context, path, contentType string, body []byte) ([]byte, error) {
	target := c.resolve(path)
	var respBody []byte

	err := retry.Do(ctx, c.retryPolicy, func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			// Construction errors are not transport flakes — do not retry.
			return retry.Permanent(fmt.Errorf("ingest: build request: %w", err))
		}
		c.applyHeaders(req, contentType)

		resp, err := c.http.Do(req)
		if err != nil {
			// Transport / DNS / TLS errors are retried.
			return fmt.Errorf("ingest: transport: %w", err)
		}
		defer resp.Body.Close()

		read, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fmt.Errorf("ingest: read body: %w", readErr)
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			respBody = read
			return nil
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			return retry.Permanent(httpError(resp.StatusCode, read))
		default:
			return httpError(resp.StatusCode, read)
		}
	})

	if err != nil {
		return nil, err
	}
	return respBody, nil
}

func (c *Client) resolve(path string) string {
	// Avoid double-slashes from joining BaseURL with an absolute path.
	base := strings.TrimRight(c.baseURL.String(), "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

func (c *Client) applyHeaders(req *http.Request, contentType string) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-Incidentary-Agent-Version", c.agentVersion)
	req.Header.Set("User-Agent", c.userAgent)
	if c.surface != 0 {
		req.Header.Set("X-Incidentary-Surface", c.surface.String())
	}
}

// HTTPError carries status + truncated body for a non-retryable response.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("ingest: HTTP %d: %s", e.StatusCode, e.Body)
}

func httpError(status int, body []byte) error {
	const cap = 1024
	out := body
	if len(out) > cap {
		out = out[:cap]
	}
	return &HTTPError{StatusCode: status, Body: string(out)}
}
