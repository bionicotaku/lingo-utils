package gclog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// FlushFunc allows the caller to flush buffered log entries.
type FlushFunc func(context.Context) error

// Options defines logger configuration.
type Options struct {
	Service           string
	Version           string
	ComponentTypes    map[string]struct{}
	InstanceID        string
	DisableInstanceID bool
	Writer            io.Writer
}

// Option customises Options.
type Option func(*Options)

// WithService sets the service field (required).
func WithService(name string) Option {
	return func(o *Options) {
		o.Service = name
	}
}

// WithVersion sets the version field (required).
func WithVersion(version string) Option {
	return func(o *Options) {
		o.Version = version
	}
}

// WithComponentTypes registers legal component names. When empty, any component is accepted.
func WithComponentTypes(components ...string) Option {
	return func(o *Options) {
		if o.ComponentTypes == nil {
			o.ComponentTypes = make(map[string]struct{}, len(components))
		}
		for _, c := range components {
			if c == "" {
				continue
			}
			o.ComponentTypes[c] = struct{}{}
		}
	}
}

// WithInstanceID overrides the default instance identifier.
func WithInstanceID(id string) Option {
	return func(o *Options) {
		o.InstanceID = id
	}
}

// DisableInstanceID disables emitting the instance identifier label.
func DisableInstanceID() Option {
	return func(o *Options) {
		o.DisableInstanceID = true
	}
}

// WithWriter configures the underlying writer (defaults to stdout).
func WithWriter(w io.Writer) Option {
	return func(o *Options) {
		o.Writer = w
	}
}

// ValidateOptions ensures mandatory fields are provided and populates defaults.
func ValidateOptions(opts *Options) error {
	if opts.Service == "" {
		return errors.New("gclog: service is required")
	}
	if opts.Version == "" {
		return errors.New("gclog: version is required")
	}
	if !opts.DisableInstanceID && opts.InstanceID == "" {
		if host, err := os.Hostname(); err == nil {
			opts.InstanceID = host
		}
	}
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	return nil
}

// NewLogger constructs a Logger that satisfies the Kratos log.Logger interface.
func NewLogger(opts ...Option) (log.Logger, FlushFunc, error) {
	cfg := &Options{}
	for _, opt := range opts {
		opt(cfg)
	}
	if err := ValidateOptions(cfg); err != nil {
		return nil, nil, err
	}
	l := &Logger{
		opts: *cfg,
		w:    cfg.Writer,
	}
	l.allowedKeys = map[string]struct{}{
		log.DefaultMessageKey: {},
		"trace_id":           {},
		"span_id":            {},
		"component":          {},
		"payload":            {},
	}
	return l, func(context.Context) error { return nil }, nil
}

// Logger implements the Kratos log.Logger interface and emits Cloud Logging compatible JSON.
type Logger struct {
	opts        Options
	w           io.Writer
	mu          sync.Mutex
	allowedKeys map[string]struct{}
}

// Log satisfies log.Logger.
func (l *Logger) Log(level log.Level, keyvals ...interface{}) error {
	if len(keyvals)%2 != 0 {
		keyvals = append(keyvals, nil)
	}

	var (
		msg           string
		traceID       string
		spanID        string
		component     string
		customPayload map[string]any
	)

	payload := make(map[string]any)

	for i := 0; i < len(keyvals); i += 2 {
		key := fmt.Sprint(keyvals[i])
		if _, ok := l.allowedKeys[key]; !ok {
			return fmt.Errorf("gclog: unsupported log field %q - use payload for custom data", key)
		}
		val := keyvals[i+1]

		switch key {
	case log.DefaultMessageKey:
			msg, _ = val.(string)
		case "trace_id":
			traceID, _ = val.(string)
		case "span_id":
			spanID, _ = val.(string)
		case "component":
			if s, ok := val.(string); ok && s != "" {
				if len(l.opts.ComponentTypes) == 0 {
					component = s
				} else if _, allowed := l.opts.ComponentTypes[s]; allowed {
					component = s
				} else {
					payload["component_status"] = fmt.Sprintf("invalid:%s", s)
				}
			}
		case "payload":
			if val == nil {
				customPayload = nil
				continue
			}
			m, ok := val.(map[string]any)
			if !ok {
				return errors.New("gclog: payload must be map[string]any")
			}
			customPayload = m
		}
	}

	if msg == "" {
		msg = "<no message>"
	}

	entry := logEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Severity:  severityFromLevel(level),
		Message:   msg,
		ServiceContext: serviceContext{
			Service: l.opts.Service,
			Version: l.opts.Version,
		},
		Trace:  traceID,
		SpanID: spanID,
	}

	labels := map[string]string{}
	if component != "" {
		labels["component"] = component
	}
	if !l.opts.DisableInstanceID && l.opts.InstanceID != "" {
		labels["instance_id"] = l.opts.InstanceID
	}
	if len(labels) > 0 {
		entry.Labels = labels
	}

	if len(customPayload) > 0 {
		payload["payload"] = customPayload
	}
	if len(payload) > 0 {
		entry.JSONPayload = payload
	}

	return l.write(entry)
}

func (l *Logger) write(entry logEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.w.Write(append(data, '\n'))
	return err
}

type logEntry struct {
	Timestamp      string            `json:"timestamp"`
	Severity       string            `json:"severity"`
	Message        string            `json:"message"`
	ServiceContext serviceContext    `json:"serviceContext"`
	Trace          string            `json:"trace,omitempty"`
	SpanID         string            `json:"spanId,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	JSONPayload    map[string]any    `json:"jsonPayload,omitempty"`
}

type serviceContext struct {
	Service string `json:"service"`
	Version string `json:"version"`
}

func severityFromLevel(level log.Level) string {
	switch level {
	case log.LevelDebug:
		return "DEBUG"
	case log.LevelWarn:
		return "WARNING"
	case log.LevelError:
		return "ERROR"
	case log.LevelFatal:
		return "CRITICAL"
	default:
		return "INFO"
	}
}
