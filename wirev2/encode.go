package wirev2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// MaxEventsPerBatch matches the server-side cap (wire-format-v2 §4.1).
const MaxEventsPerBatch = 10000

// Encode marshals a batch to JSON. It performs cheap shape validation that
// catches the most common construction mistakes before the payload ever
// touches the network. Server-side validation remains authoritative.
func Encode(batch *Batch) ([]byte, error) {
	if batch == nil {
		return nil, errors.New("wirev2: nil batch")
	}
	if batch.SpecVersion == "" {
		batch.SpecVersion = SpecVersion
	}
	if batch.SpecVersion != SpecVersion {
		return nil, fmt.Errorf("wirev2: specversion %q is not supported by this encoder", batch.SpecVersion)
	}
	if batch.FlushedAt <= 0 {
		return nil, errors.New("wirev2: flushed_at must be > 0")
	}
	if batch.CaptureMode != CaptureSkeleton && batch.CaptureMode != CaptureFull {
		return nil, fmt.Errorf("wirev2: invalid capture_mode %q", batch.CaptureMode)
	}
	if len(batch.Events) == 0 {
		return nil, errors.New("wirev2: batch must contain at least one event")
	}
	if len(batch.Events) > MaxEventsPerBatch {
		return nil, fmt.Errorf("wirev2: batch has %d events, max %d", len(batch.Events), MaxEventsPerBatch)
	}
	if batch.Agent.Version == "" {
		return nil, errors.New("wirev2: agent.version is required")
	}
	if batch.Agent.WorkspaceID == "" {
		return nil, errors.New("wirev2: agent.workspace_id is required")
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(batch); err != nil {
		return nil, fmt.Errorf("wirev2: encode batch: %w", err)
	}
	// json.Encoder appends a trailing newline; drop it for a clean payload.
	out := buf.Bytes()
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	return out, nil
}
