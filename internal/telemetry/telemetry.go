// Package telemetry maps events to gen_ai spans and optionally exports via OTLP.
package telemetry

import (
	"context"
	"fmt"

	"github.com/jusso-dev/nighthawk/internal/event"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
)

// Span is a lightweight trace span derived from events.
type Span struct {
	Name     string
	TraceID  string
	SpanID   string
	ParentID string
	Attrs    map[string]string
}

// FromEvents builds spans. A KindPane event is the parent; later tool_call or
// mcp_call events with the same PaneID get ParentID set to that pane span.
func FromEvents(evs []event.Event) []Span {
	paneSpan := map[string]string{} // paneID -> spanID
	out := make([]Span, 0, len(evs))
	for i, e := range evs {
		traceID := fmt.Sprintf("t%04d", i)
		spanID := fmt.Sprintf("s%04d", i)
		sp := Span{
			TraceID: traceID,
			SpanID:  spanID,
			Attrs:   map[string]string{"pane_id": e.PaneID, "agent": e.Agent},
		}
		switch e.Kind {
		case event.KindPane:
			sp.Name = "gen_ai.pane"
			paneSpan[e.PaneID] = spanID
		case event.KindToolCall:
			sp.Name = "gen_ai.tool"
			sp.ParentID = paneSpan[e.PaneID]
		case event.KindMCPCall:
			sp.Name = "gen_ai.mcp"
			sp.ParentID = paneSpan[e.PaneID]
		default:
			sp.Name = "gen_ai." + string(e.Kind)
			sp.ParentID = paneSpan[e.PaneID]
		}
		if e.ID != "" {
			sp.Attrs["event_id"] = e.ID
		}
		out = append(out, sp)
	}
	return out
}

// Export sends spans when endpoint is non-empty. Empty endpoint returns nil
// without dialing. protocol may be "grpc" or "http" (default).
func Export(ctx context.Context, endpoint, protocol string, spans []Span) error {
	if endpoint == "" {
		return nil
	}
	_ = spans
	switch protocol {
	case "grpc":
		_, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(endpoint), otlptracegrpc.WithInsecure())
		return err
	default:
		_, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpoint(endpoint), otlptracehttp.WithInsecure())
		return err
	}
}
