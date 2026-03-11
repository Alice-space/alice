package platform

import (
	"context"
	"time"
)

// Context keys for telemetry.
type contextKey string

const (
	traceIDKey   contextKey = "trace_id"
	requestIDKey contextKey = "request_id"
	taskIDKey    contextKey = "task_id"
	eventIDKey   contextKey = "event_id"
	startTimeKey contextKey = "start_time"
)

// ContextWithTraceID returns a new context with trace ID.
func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// ContextWithRequestID returns a new context with request ID.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// ContextWithTaskID returns a new context with task ID.
func ContextWithTaskID(ctx context.Context, taskID string) context.Context {
	return context.WithValue(ctx, taskIDKey, taskID)
}

// ContextWithEventID returns a new context with event ID.
func ContextWithEventID(ctx context.Context, eventID string) context.Context {
	return context.WithValue(ctx, eventIDKey, eventID)
}

// ContextWithStartTime returns a new context with start time.
func ContextWithStartTime(ctx context.Context, t time.Time) context.Context {
	return context.WithValue(ctx, startTimeKey, t)
}

// TraceIDFromContext extracts trace ID from context.
func TraceIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}

// RequestIDFromContext extracts request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// TaskIDFromContext extracts task ID from context.
func TaskIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(taskIDKey).(string); ok {
		return id
	}
	return ""
}

// EventIDFromContext extracts event ID from context.
func EventIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(eventIDKey).(string); ok {
		return id
	}
	return ""
}

// StartTimeFromContext extracts start time from context.
func StartTimeFromContext(ctx context.Context) (time.Time, bool) {
	if t, ok := ctx.Value(startTimeKey).(time.Time); ok {
		return t, true
	}
	return time.Time{}, false
}

// DurationFromContext calculates duration from start time in context.
func DurationFromContext(ctx context.Context) (time.Duration, bool) {
	if start, ok := StartTimeFromContext(ctx); ok {
		return time.Since(start), true
	}
	return 0, false
}

// DurationMSFromContext calculates duration in milliseconds.
func DurationMSFromContext(ctx context.Context) int64 {
	if d, ok := DurationFromContext(ctx); ok {
		return d.Milliseconds()
	}
	return 0
}

// TelemetryContext creates a base telemetry context with trace ID and start time.
func TelemetryContext(ctx context.Context, traceID string) context.Context {
	ctx = ContextWithTraceID(ctx, traceID)
	ctx = ContextWithStartTime(ctx, time.Now().UTC())
	return ctx
}

// OperationLogAttrs returns common log attributes from context.
func OperationLogAttrs(ctx context.Context) []any {
	var attrs []any
	if id := TraceIDFromContext(ctx); id != "" {
		attrs = append(attrs, "trace_id", id)
	}
	if id := RequestIDFromContext(ctx); id != "" {
		attrs = append(attrs, "request_id", id)
	}
	if id := TaskIDFromContext(ctx); id != "" {
		attrs = append(attrs, "task_id", id)
	}
	if id := EventIDFromContext(ctx); id != "" {
		attrs = append(attrs, "event_id", id)
	}
	if d := DurationMSFromContext(ctx); d > 0 {
		attrs = append(attrs, "duration_ms", d)
	}
	return attrs
}
