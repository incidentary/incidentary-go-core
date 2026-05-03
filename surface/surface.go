// Package surface provides the AgentSurface and AgentType enums plus the
// D9 derivation table that maps an agent's implementation identity to the
// fidelity-contract surfaces it is allowed to declare.
//
// The authoritative server-side implementation lives at
// Incidentary/apps/api/src/domain/v2/enums.rs. This package mirrors that
// derivation so that Go-based agents can validate locally before attempting
// a flush.
package surface

import (
	"encoding/json"
	"fmt"
)

// AgentType is the agent's implementation identity. Wire string lives in
// the lowercase snake_case forms used by the v2 wire format.
type AgentType uint8

const (
	AgentTypeSDK AgentType = iota + 1
	AgentTypeK8sOperator
	AgentTypeCloudWebhook
	AgentTypeCICDWebhook
	AgentTypeIncidentWebhook
	AgentTypeOTLPBridge
)

// String returns the wire form for the agent type.
func (a AgentType) String() string {
	switch a {
	case AgentTypeSDK:
		return "sdk"
	case AgentTypeK8sOperator:
		return "k8s_operator"
	case AgentTypeCloudWebhook:
		return "cloud_webhook"
	case AgentTypeCICDWebhook:
		return "cicd_webhook"
	case AgentTypeIncidentWebhook:
		return "incident_webhook"
	case AgentTypeOTLPBridge:
		return "otlp_bridge"
	}
	return "unknown"
}

// MarshalJSON serializes to the wire string form.
func (a AgentType) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

// UnmarshalJSON parses the wire string form.
func (a *AgentType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseAgentType(s)
	if err != nil {
		return err
	}
	*a = parsed
	return nil
}

// ParseAgentType returns the AgentType matching the wire string, or an error
// when the value is unknown.
func ParseAgentType(s string) (AgentType, error) {
	switch s {
	case "sdk":
		return AgentTypeSDK, nil
	case "k8s_operator":
		return AgentTypeK8sOperator, nil
	case "cloud_webhook":
		return AgentTypeCloudWebhook, nil
	case "cicd_webhook":
		return AgentTypeCICDWebhook, nil
	case "incident_webhook":
		return AgentTypeIncidentWebhook, nil
	case "otlp_bridge":
		return AgentTypeOTLPBridge, nil
	}
	return 0, fmt.Errorf("surface: unknown agent type %q", s)
}

// AgentSurface is the fidelity-contract identity. Wire form is lowercase.
type AgentSurface uint8

const (
	SurfaceExporter AgentSurface = iota + 1
	SurfaceProcessor
	SurfaceSDK
	SurfaceOperator
)

// String returns the wire form.
func (s AgentSurface) String() string {
	switch s {
	case SurfaceExporter:
		return "exporter"
	case SurfaceProcessor:
		return "processor"
	case SurfaceSDK:
		return "sdk"
	case SurfaceOperator:
		return "operator"
	}
	return "unknown"
}

// MarshalJSON serializes to the wire string form.
func (s AgentSurface) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON parses the wire string form.
func (s *AgentSurface) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	parsed, err := ParseAgentSurface(raw)
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

// ParseAgentSurface returns the AgentSurface matching the wire string, or an
// error when the value is unknown.
func ParseAgentSurface(s string) (AgentSurface, error) {
	switch s {
	case "exporter":
		return SurfaceExporter, nil
	case "processor":
		return SurfaceProcessor, nil
	case "sdk":
		return SurfaceSDK, nil
	case "operator":
		return SurfaceOperator, nil
	}
	return 0, fmt.Errorf("surface: unknown agent surface %q", s)
}

// MismatchError is returned by Derive when a declared surface is not allowed
// for the given agent type. Callers should surface this as an actionable
// HTTP 400 in the same shape the server uses (see the Phase 2 plan).
type MismatchError struct {
	AgentType AgentType
	Declared  AgentSurface
	Allowed   []AgentSurface
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf(
		"surface: agent.surface=%s is not allowed for agent.type=%s; allowed: %s",
		e.Declared, e.AgentType, formatSurfaces(e.Allowed),
	)
}

func formatSurfaces(ss []AgentSurface) string {
	out := "["
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s.String()
	}
	return out + "]"
}

// AllowedFor returns the D9 allow-list for the given agent type.
//
// The first entry is also the default when the wire payload omits an
// explicit surface — the most-conservative interpretation wins.
func AllowedFor(agentType AgentType) []AgentSurface {
	switch agentType {
	case AgentTypeSDK:
		return []AgentSurface{SurfaceSDK}
	case AgentTypeK8sOperator:
		return []AgentSurface{SurfaceOperator}
	case AgentTypeOTLPBridge:
		return []AgentSurface{SurfaceExporter, SurfaceProcessor}
	case AgentTypeCloudWebhook, AgentTypeCICDWebhook, AgentTypeIncidentWebhook:
		return []AgentSurface{SurfaceExporter}
	}
	return nil
}

// Derive resolves the effective surface for a batch.
//
// When declared is the zero value (no surface field on the wire), the
// default is the first entry in the D9 allow-list for the agent type.
// When a non-zero declared value is not in the allow-list, Derive returns
// a *MismatchError.
func Derive(agentType AgentType, declared AgentSurface) (AgentSurface, error) {
	allowed := AllowedFor(agentType)
	if len(allowed) == 0 {
		return 0, fmt.Errorf("surface: unknown agent type %d", agentType)
	}
	if declared == 0 {
		return allowed[0], nil
	}
	for _, s := range allowed {
		if s == declared {
			return declared, nil
		}
	}
	return 0, &MismatchError{
		AgentType: agentType,
		Declared:  declared,
		Allowed:   allowed,
	}
}
