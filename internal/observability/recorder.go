package observability

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

type Recorder struct {
	db      *gorm.DB
	service string
}

func NewRecorder(db *gorm.DB, service string) *Recorder {
	return &Recorder{
		db:      db,
		service: service,
	}
}

// 采用option模式避免函数签名越来越长

type LogOption func(*LogEntry)

func WithRoute(route string) LogOption {
	return func(e *LogEntry) {
		e.Route = route
	}
}

func WithMethod(method string) LogOption {
	return func(e *LogEntry) {
		e.Method = method
	}
}

func WithAttrs(attrs map[string]any) LogOption {
	return func(e *LogEntry) {
		if len(attrs) == 0 {
			return
		}
		if data, err := json.Marshal(attrs); err == nil {
			e.Attrs = string(data)
		} else {
			log.Println("marshal log attrs failed:", err)
		}
	}
}
func (r *Recorder) Record(ctx context.Context, level, template string, opts ...LogOption) {
	if r == nil {
		// 可选：记录日志或直接返回
		log.Println("Recorder is nil, skip recording")
		return
	}
	entry := LogEntry{
		Ts:       time.Now().UnixMilli(),
		Service:  r.service,
		Level:    level,
		Template: template,
		Route:    RouteFromContext(ctx),
		Method:   MethodFromContext(ctx),
		Attrs:    "{}",
		TraceID:  traceIDFromContext(ctx),
	}
	for _, opt := range opts {
		opt(&entry)
	}
	if r.db == nil {
		return
	}
	if err := r.db.WithContext(ctx).Create(&entry).Error; err != nil {
		log.Println("record event failed:", err)
	}
}

func traceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
