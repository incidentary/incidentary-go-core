package wirev2

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/incidentary/incidentary-go-core/surface"
)

func makeMinimalBatch() *Batch {
	statusCode := int32(500)
	durationNs := int64(340000000)
	return &Batch{
		Resource: Resource{
			ServiceName:       "payment-service",
			DeployEnvironment: "production",
		},
		Agent: Agent{
			Type:        surface.AgentTypeOTLPBridge,
			Version:     "0.0.1",
			Language:    "go",
			WorkspaceID: "org_abc123",
			Surface:     surface.SurfaceProcessor,
		},
		CaptureMode: CaptureSkeleton,
		FlushedAt:   1733103000000000000,
		Events: []Event{{
			ID:         "550e8400-e29b-41d4-a716-446655440000",
			Kind:       "HTTP_SERVER",
			Severity:   SeverityError,
			OccurredAt: 1733103000000000001,
			ServiceID:  "payment-service",
			TraceID:    "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
			StatusCode: &statusCode,
			DurationNs: &durationNs,
		}},
	}
}

func TestEncodeRoundTripMinimalBatch(t *testing.T) {
	batch := makeMinimalBatch()

	raw, err := Encode(batch)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	// Decode into an untyped map so we can pin the on-the-wire shape.
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := decoded["specversion"]; got != "2" {
		t.Fatalf("specversion = %v, want \"2\"", got)
	}
	if got := decoded["capture_mode"]; got != "SKELETON" {
		t.Fatalf("capture_mode = %v, want SKELETON", got)
	}

	resource := decoded["resource"].(map[string]any)
	if resource["service.name"] != "payment-service" {
		t.Fatalf("resource.service.name = %v", resource["service.name"])
	}
	if resource["deployment.environment"] != "production" {
		t.Fatalf("resource.deployment.environment = %v", resource["deployment.environment"])
	}

	agent := decoded["agent"].(map[string]any)
	if agent["type"] != "otlp_bridge" {
		t.Fatalf("agent.type = %v, want otlp_bridge", agent["type"])
	}
	if agent["surface"] != "processor" {
		t.Fatalf("agent.surface = %v, want processor", agent["surface"])
	}

	events := decoded["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	ev := events[0].(map[string]any)
	if ev["kind"] != "HTTP_SERVER" {
		t.Fatalf("event.kind = %v", ev["kind"])
	}
	if ev["severity"] != "error" {
		t.Fatalf("event.severity = %v", ev["severity"])
	}
}

func TestEncodeOmitsSurfaceWhenZero(t *testing.T) {
	batch := makeMinimalBatch()
	batch.Agent.Surface = 0 // let the server derive

	raw, err := Encode(batch)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(raw), "\"surface\"") {
		t.Fatalf("expected surface field to be omitted, got: %s", raw)
	}
}

func TestEncodeRejectsEmptyBatch(t *testing.T) {
	batch := makeMinimalBatch()
	batch.Events = nil
	if _, err := Encode(batch); err == nil {
		t.Fatalf("expected error for empty events")
	}
}

func TestEncodeRejectsZeroFlushedAt(t *testing.T) {
	batch := makeMinimalBatch()
	batch.FlushedAt = 0
	if _, err := Encode(batch); err == nil {
		t.Fatalf("expected error for zero flushed_at")
	}
}

func TestEncodeRejectsBadCaptureMode(t *testing.T) {
	batch := makeMinimalBatch()
	batch.CaptureMode = "PARTIAL"
	if _, err := Encode(batch); err == nil {
		t.Fatalf("expected error for invalid capture_mode")
	}
}

func TestEncodeRejectsMissingAgentMetadata(t *testing.T) {
	batch := makeMinimalBatch()
	batch.Agent.Version = ""
	if _, err := Encode(batch); err == nil {
		t.Fatalf("expected error for empty agent.version")
	}

	batch = makeMinimalBatch()
	batch.Agent.WorkspaceID = ""
	if _, err := Encode(batch); err == nil {
		t.Fatalf("expected error for empty workspace_id")
	}
}

func TestEncodeNilBatch(t *testing.T) {
	if _, err := Encode(nil); err == nil {
		t.Fatalf("expected error for nil batch")
	}
}

func TestEncodeRejectsTooManyEvents(t *testing.T) {
	batch := makeMinimalBatch()
	original := batch.Events[0]
	batch.Events = make([]Event, MaxEventsPerBatch+1)
	for i := range batch.Events {
		batch.Events[i] = original
	}
	if _, err := Encode(batch); err == nil {
		t.Fatalf("expected error for batch over MaxEventsPerBatch")
	}
}

func TestResourceExtraMergedWithoutOverridingExplicit(t *testing.T) {
	batch := makeMinimalBatch()
	batch.Resource.Extra = map[string]any{
		"telemetry.sdk.name": "opentelemetry",
		// Extra MUST NOT override explicit fields:
		"service.name": "should-be-ignored",
	}
	raw, err := Encode(batch)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resource := decoded["resource"].(map[string]any)
	if resource["service.name"] != "payment-service" {
		t.Fatalf("explicit field should win, got service.name=%v", resource["service.name"])
	}
	if resource["telemetry.sdk.name"] != "opentelemetry" {
		t.Fatalf("expected merged extra field; got %v", resource["telemetry.sdk.name"])
	}
}
