package callback

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestTracer_HierarchicalSpans(t *testing.T) {
	ctx := context.Background()
	tracer := NewTracer("test-trace-123")

	// 1. Root Workflow Span
	ctx, rootSpan := tracer.StartSpan(ctx, "agent_workflow", SpanTypeWorkflow)
	rootSpan.SetAttribute("goal", "Search and compute")
	time.Sleep(2 * time.Millisecond)

	// 2. Child SuperStep Span
	ctxStep, stepSpan := tracer.StartSpan(ctx, "step_0", SpanTypeSuperStep)
	time.Sleep(1 * time.Millisecond)

	// 3. Child Node Span
	_, nodeSpan := tracer.StartSpan(ctxStep, "model_node", SpanTypeNode)
	nodeSpan.SetAttribute("model", "qwen2.5")
	time.Sleep(1 * time.Millisecond)
	tracer.EndSpan(nodeSpan)

	// 4. Child Tool Span
	_, toolSpan := tracer.StartSpan(ctxStep, "web_search", SpanTypeTool)
	toolSpan.SetAttribute("query", "golang concurrency")
	time.Sleep(1 * time.Millisecond)
	tracer.EndSpan(toolSpan, errors.New("rate limited"))

	tracer.EndSpan(stepSpan)
	tracer.EndSpan(rootSpan)

	spans := tracer.Spans()
	if len(spans) != 4 {
		t.Fatalf("expected 4 spans, got %d", len(spans))
	}

	// Verify ParentSpanIDs
	if rootSpan.ParentSpanID != "" {
		t.Fatalf("root span should have empty ParentSpanID, got %s", rootSpan.ParentSpanID)
	}
	if stepSpan.ParentSpanID != rootSpan.SpanID {
		t.Fatalf("step span parent should be %s, got %s", rootSpan.SpanID, stepSpan.ParentSpanID)
	}
	if nodeSpan.ParentSpanID != stepSpan.SpanID {
		t.Fatalf("node span parent should be %s, got %s", stepSpan.SpanID, nodeSpan.ParentSpanID)
	}
	if toolSpan.ParentSpanID != stepSpan.SpanID {
		t.Fatalf("tool span parent should be %s, got %s", stepSpan.SpanID, toolSpan.ParentSpanID)
	}

	// Verify status & attributes
	if toolSpan.Status != "ERROR" || toolSpan.Error != "rate limited" {
		t.Fatalf("expected tool span error status, got %s / %s", toolSpan.Status, toolSpan.Error)
	}
	if rootSpan.Attributes["goal"] != "Search and compute" {
		t.Fatalf("unexpected attribute: %v", rootSpan.Attributes["goal"])
	}

	// Verify JSON serialization
	jsonData, err := tracer.ToJSON()
	if err != nil {
		t.Fatalf("json serialization error: %v", err)
	}
	if !strings.Contains(string(jsonData), `"test-trace-123"`) {
		t.Fatalf("expected trace id in json: %s", string(jsonData))
	}

	// Verify PrintTree output
	var buf bytes.Buffer
	tracer.PrintTree(&buf)
	treeOutput := buf.String()
	if !strings.Contains(treeOutput, "[workflow] \"agent_workflow\"") ||
		!strings.Contains(treeOutput, "[tool] \"web_search\"") {
		t.Fatalf("unexpected tree output:\n%s", treeOutput)
	}
}
