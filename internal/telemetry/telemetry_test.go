package telemetry

import (
	"context"
	"testing"

	"github.com/jusso-dev/nighthawk/internal/event"
)

func TestFromEventsParent(t *testing.T) {
	evs := []event.Event{
		{Kind: event.KindPane, PaneID: "p1", Agent: "claude", ID: "e0"},
		{Kind: event.KindToolCall, PaneID: "p1", Agent: "claude", ID: "e1"},
		{Kind: event.KindMCPCall, PaneID: "p1", Agent: "claude", ID: "e2"},
		{Kind: event.KindToolCall, PaneID: "other", Agent: "x", ID: "e3"},
	}
	spans := FromEvents(evs)
	if len(spans) != 4 {
		t.Fatalf("%d", len(spans))
	}
	if spans[0].Name != "gen_ai.pane" || spans[0].ParentID != "" {
		t.Fatalf("%+v", spans[0])
	}
	if spans[1].Name != "gen_ai.tool" || spans[1].ParentID != spans[0].SpanID {
		t.Fatalf("%+v", spans[1])
	}
	if spans[2].Name != "gen_ai.mcp" || spans[2].ParentID != spans[0].SpanID {
		t.Fatalf("%+v", spans[2])
	}
	if spans[3].ParentID != "" {
		t.Fatalf("orphan should have empty parent: %+v", spans[3])
	}
}

func TestExportEmptyEndpoint(t *testing.T) {
	if err := Export(context.Background(), "", "http", nil); err != nil {
		t.Fatal(err)
	}
	if err := Export(context.Background(), "", "grpc", nil); err != nil {
		t.Fatal(err)
	}
}
