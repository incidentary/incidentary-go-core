package surface

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestDeriveDefault pins the D9 derivation table for the case where the
// wire payload does NOT carry an explicit surface field. Mirrors the Rust
// `agent_surface_default_from_agent_type` test.
func TestDeriveDefault(t *testing.T) {
	cases := []struct {
		name string
		in   AgentType
		want AgentSurface
	}{
		{"sdk -> sdk", AgentTypeSDK, SurfaceSDK},
		{"k8s_operator -> operator", AgentTypeK8sOperator, SurfaceOperator},
		{"otlp_bridge -> exporter", AgentTypeOTLPBridge, SurfaceExporter},
		{"cloud_webhook -> exporter", AgentTypeCloudWebhook, SurfaceExporter},
		{"cicd_webhook -> exporter", AgentTypeCICDWebhook, SurfaceExporter},
		{"incident_webhook -> exporter", AgentTypeIncidentWebhook, SurfaceExporter},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Derive(tc.in, 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("Derive(%s, none) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

// TestDeriveAllowedDeclarations covers every (agent_type, allowed_surface)
// pair from the D9 table.
func TestDeriveAllowedDeclarations(t *testing.T) {
	cases := []struct {
		agentType AgentType
		surface   AgentSurface
	}{
		{AgentTypeSDK, SurfaceSDK},
		{AgentTypeK8sOperator, SurfaceOperator},
		{AgentTypeOTLPBridge, SurfaceExporter},
		{AgentTypeOTLPBridge, SurfaceProcessor},
		{AgentTypeCloudWebhook, SurfaceExporter},
		{AgentTypeCICDWebhook, SurfaceExporter},
		{AgentTypeIncidentWebhook, SurfaceExporter},
	}
	for _, tc := range cases {
		got, err := Derive(tc.agentType, tc.surface)
		if err != nil {
			t.Fatalf("Derive(%s, %s) returned error: %v", tc.agentType, tc.surface, err)
		}
		if got != tc.surface {
			t.Fatalf("Derive(%s, %s) = %s, want %s", tc.agentType, tc.surface, got, tc.surface)
		}
	}
}

// TestDeriveRejectsForbiddenDeclaration covers the negative cases — declared
// surfaces that are not in the agent type's allow-list.
func TestDeriveRejectsForbiddenDeclaration(t *testing.T) {
	cases := []struct {
		name      string
		agentType AgentType
		declared  AgentSurface
	}{
		{"otlp_bridge cannot self-declare sdk", AgentTypeOTLPBridge, SurfaceSDK},
		{"sdk cannot declare operator", AgentTypeSDK, SurfaceOperator},
		{"k8s_operator cannot declare exporter", AgentTypeK8sOperator, SurfaceExporter},
		{"cloud_webhook cannot declare processor", AgentTypeCloudWebhook, SurfaceProcessor},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Derive(tc.agentType, tc.declared)
			if err == nil {
				t.Fatalf("expected MismatchError, got nil")
			}
			var mm *MismatchError
			if !errors.As(err, &mm) {
				t.Fatalf("expected *MismatchError, got %T (%v)", err, err)
			}
			if mm.AgentType != tc.agentType || mm.Declared != tc.declared {
				t.Fatalf("mismatch payload: agent=%s declared=%s", mm.AgentType, mm.Declared)
			}
			if len(mm.Allowed) == 0 {
				t.Fatalf("expected non-empty Allowed list")
			}
			if mm.Error() == "" {
				t.Fatalf("expected non-empty error message")
			}
		})
	}
}

func TestAgentTypeJSONRoundTrip(t *testing.T) {
	cases := []AgentType{
		AgentTypeSDK,
		AgentTypeK8sOperator,
		AgentTypeCloudWebhook,
		AgentTypeCICDWebhook,
		AgentTypeIncidentWebhook,
		AgentTypeOTLPBridge,
	}
	for _, in := range cases {
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal %s: %v", in, err)
		}
		var got AgentType
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if got != in {
			t.Fatalf("round-trip %s -> %s", in, got)
		}
	}
}

func TestAgentSurfaceJSONLowercase(t *testing.T) {
	cases := []struct {
		s    AgentSurface
		json string
	}{
		{SurfaceExporter, `"exporter"`},
		{SurfaceProcessor, `"processor"`},
		{SurfaceSDK, `"sdk"`},
		{SurfaceOperator, `"operator"`},
	}
	for _, tc := range cases {
		raw, err := json.Marshal(tc.s)
		if err != nil {
			t.Fatalf("marshal %s: %v", tc.s, err)
		}
		if string(raw) != tc.json {
			t.Fatalf("marshal %s = %s, want %s", tc.s, raw, tc.json)
		}
		var got AgentSurface
		if err := json.Unmarshal([]byte(tc.json), &got); err != nil {
			t.Fatalf("unmarshal %s: %v", tc.json, err)
		}
		if got != tc.s {
			t.Fatalf("unmarshal %s = %s", tc.json, got)
		}
	}
}

func TestParseAgentTypeUnknown(t *testing.T) {
	if _, err := ParseAgentType("not-a-real-type"); err == nil {
		t.Fatalf("expected error for unknown agent type")
	}
}

func TestParseAgentSurfaceUnknown(t *testing.T) {
	if _, err := ParseAgentSurface("supervisor"); err == nil {
		t.Fatalf("expected error for unknown surface")
	}
}
