// Package wirev2 implements the Incidentary v2 wire-format types and a
// JSON encoder that produces payloads conformant to
// `incidentary-wire-format/spec/wire-format-v2.md`.
//
// The encoder is intentionally minimal — it does not own per-kind attribute
// schemas, transport, or auth. Those concerns belong to ingest/, retry/,
// and the agent-specific code (e.g., the OpenTelemetry Bridge).
package wirev2

import (
	"encoding/json"

	"github.com/incidentary/incidentary-go-core/surface"
)

// SpecVersion is the only specversion this encoder produces.
const SpecVersion = "2"

// CaptureMode declares what fidelity of data the agent captured.
type CaptureMode string

const (
	CaptureSkeleton CaptureMode = "SKELETON"
	CaptureFull     CaptureMode = "FULL"
)

// Severity matches the wire-format v2 severity enum.
type Severity string

const (
	SeverityTrace   Severity = "trace"
	SeverityDebug   Severity = "debug"
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
	SeverityFatal   Severity = "fatal"
)

// Batch is the top-level payload posted to POST /api/v2/ingest.
type Batch struct {
	SpecVersion string      `json:"specversion"`
	Resource    Resource    `json:"resource"`
	Agent       Agent       `json:"agent"`
	CaptureMode CaptureMode `json:"capture_mode"`
	FlushedAt   int64       `json:"flushed_at"`
	SchemaURL   *string     `json:"schema_url,omitempty"`
	Events      []Event     `json:"events"`
}

// Resource describes the entity emitting the batch. It uses OpenTelemetry
// semantic-convention dot-notation keys; we model the common fields
// explicitly and let everything else flow through Extra.
type Resource struct {
	ServiceName       string `json:"service.name,omitempty"`
	ServiceVersion    string `json:"service.version,omitempty"`
	ServiceNamespace  string `json:"service.namespace,omitempty"`
	ServiceGitSHA     string `json:"service.git_sha,omitempty"`
	DeployEnvironment string `json:"deployment.environment,omitempty"`
	HostName          string `json:"host.name,omitempty"`
	K8sClusterName    string `json:"k8s.cluster.name,omitempty"`
	K8sNamespaceName  string `json:"k8s.namespace.name,omitempty"`
	K8sPodName        string `json:"k8s.pod.name,omitempty"`
	K8sPodUID         string `json:"k8s.pod.uid,omitempty"`
	K8sDeploymentName string `json:"k8s.deployment.name,omitempty"`
	K8sNodeName       string `json:"k8s.node.name,omitempty"`
	CloudProvider     string `json:"cloud.provider,omitempty"`
	CloudRegion       string `json:"cloud.region,omitempty"`
	CloudAccountID    string `json:"cloud.account.id,omitempty"`
	CloudPlatform     string `json:"cloud.platform,omitempty"`
	CloudAvailability string `json:"cloud.availability_zone,omitempty"`
	// Extra carries any additional resource attributes the caller wants on
	// the wire. Keys MUST follow OTel semantic-convention dot notation.
	Extra map[string]any `json:"-"`
}

// MarshalJSON merges Extra with the explicit fields. Explicit fields win.
func (r Resource) MarshalJSON() ([]byte, error) {
	// Use an anonymous alias so we get the field-tag JSON encoding for free.
	type alias Resource
	base, err := json.Marshal(alias(r))
	if err != nil {
		return nil, err
	}
	if len(r.Extra) == 0 {
		return base, nil
	}
	merged := make(map[string]json.RawMessage)
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	for k, v := range r.Extra {
		if _, taken := merged[k]; taken {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		merged[k] = raw
	}
	return json.Marshal(merged)
}

// Agent identifies the sender of the batch. Surface is optional on the wire
// per spec D8 — when zero (the AgentSurface zero value), Surface is omitted
// and the server derives a default.
type Agent struct {
	Type        surface.AgentType    `json:"type"`
	Version     string               `json:"version"`
	Language    string               `json:"language,omitempty"`
	WorkspaceID string               `json:"workspace_id"`
	Surface     surface.AgentSurface `json:"surface,omitempty"`
	Telemetry   *AgentTelemetry      `json:"telemetry,omitempty"`
}

// agentWire is the on-the-wire representation of Agent; it lets us emit
// Surface only when non-zero.
type agentWire struct {
	Type        surface.AgentType `json:"type"`
	Version     string            `json:"version"`
	Language    string            `json:"language,omitempty"`
	WorkspaceID string            `json:"workspace_id"`
	Surface     *string           `json:"surface,omitempty"`
	Telemetry   *AgentTelemetry   `json:"telemetry,omitempty"`
}

// MarshalJSON omits the surface field when the surface is the zero value
// (i.e., the agent did not declare one — let the server derive it).
func (a Agent) MarshalJSON() ([]byte, error) {
	w := agentWire{
		Type:        a.Type,
		Version:     a.Version,
		Language:    a.Language,
		WorkspaceID: a.WorkspaceID,
		Telemetry:   a.Telemetry,
	}
	if a.Surface != 0 {
		s := a.Surface.String()
		w.Surface = &s
	}
	return json.Marshal(w)
}

// AgentTelemetry is the optional self-diagnostics block.
type AgentTelemetry struct {
	QueueDepth     int64 `json:"queue_depth,omitempty"`
	DroppedCECount int64 `json:"dropped_ce_count,omitempty"`
	FlushLatencyMs int64 `json:"flush_latency_ms,omitempty"`
	RingBufferSize int64 `json:"ring_buffer_size,omitempty"`
}

// Event is a single causal-event entry in the batch.
type Event struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Type       *string  `json:"type,omitempty"`
	Severity   Severity `json:"severity"`
	OccurredAt int64    `json:"occurred_at"`
	ServiceID  string   `json:"service_id,omitempty"`

	TraceID  string `json:"trace_id,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
	SpanID   string `json:"span_id,omitempty"`

	EventType  string `json:"event_type,omitempty"`
	StatusCode *int32 `json:"status_code,omitempty"`
	DurationNs *int64 `json:"duration_ns,omitempty"`

	Series     *Series         `json:"series,omitempty"`
	Attributes json.RawMessage `json:"attributes,omitempty"`
	Detail     json.RawMessage `json:"detail,omitempty"`

	CapturedBeforeAlert bool  `json:"captured_before_alert,omitempty"`
	RingBufferSeq       int64 `json:"ring_buffer_seq,omitempty"`
}

// Series is the optional deduplication block.
type Series struct {
	Count   uint32 `json:"count"`
	FirstAt int64  `json:"first_at"`
	LastAt  int64  `json:"last_at"`
}
