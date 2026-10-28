package observability

import (
	"context"
	"log"
	"sync/atomic"
)

type requestIDKey struct{}

var databaseErrors atomic.Uint64

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

func RequestID(ctx context.Context) string {
	requestID, _ := ctx.Value(requestIDKey{}).(string)
	return requestID
}

func Trace(ctx context.Context, layer, operation string) {
	log.Printf("event=trace request_id=%q layer=%q operation=%q", RequestID(ctx), layer, operation)
}

func RecordDatabaseError() {
	databaseErrors.Add(1)
}

func DatabaseErrorCount() uint64 {
	return databaseErrors.Load()
}
