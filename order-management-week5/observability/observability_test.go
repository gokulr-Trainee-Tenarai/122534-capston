package observability

import (
	"context"
	"testing"
)

func TestRequestIDContextAndDatabaseErrorCounter(t *testing.T) {
	ctx := WithRequestID(context.Background(), "request-123")
	if got := RequestID(ctx); got != "request-123" {
		t.Fatalf("expected request ID request-123, got %q", got)
	}

	before := DatabaseErrorCount()
	RecordDatabaseError()
	if got := DatabaseErrorCount(); got != before+1 {
		t.Fatalf("expected database error count %d, got %d", before+1, got)
	}
}
