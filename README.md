# incidentary-go-core

Shared Go types and helpers for first-party Incidentary agents written in Go.

## Stability

**Alpha.** Internal use only until v0.1.0. Public API may change without
deprecation notice. This module exists to support the Incidentary OpenTelemetry
Bridge and (later) the Kubernetes Operator. External consumers should pin to
a tagged commit.

## Scope

This module provides the wire-format-v2 types, ingest authentication,
retry semantics, and surface validation that are shared across Incidentary's
Go-based agents. It deliberately has **no dependency on
`go.opentelemetry.io/*`** — OTLP-specific concerns live in the Bridge.

## Packages

| Package    | Purpose                                                       |
|------------|---------------------------------------------------------------|
| `wirev2`   | V2 wire-format types and JSON encoder (Batch, Resource, Agent, Event) |
| `surface`  | `AgentSurface` and `AgentType` enums plus the D9 derivation table     |
| `ingest`   | HTTPS client wrapping retry and auth (`BatchSender`)                  |
| `retry`    | Exponential-backoff helper                                            |

The wire format spec lives at
`incidentary-wire-format/spec/wire-format-v2.md`. The server-side surface
derivation lives at
`Incidentary/apps/api/src/domain/v2/enums.rs`. This module mirrors both.

## License

Apache 2.0. See `LICENSE`.
