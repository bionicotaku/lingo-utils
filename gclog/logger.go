package gclog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

const (
	messageKey     = "message"
	traceKey       = "trace_id"
	spanKey        = "span_id"
	callerKey      = "caller"
	payloadKey     = "payload"
	labelsKey      = "labels"
	httpRequestKey = "http_request"
	errorKey       = "error"
)

var (
	kratosLabelFields = map[string]struct{}{
		"kind":      {},
		"component": {},
		"operation": {},
	}
	kratosPayloadFields = map[string]struct{}{
		"args":    {},
		"code":    {},
		"reason":  {},
		"stack":   {},
		"latency": {},
	}
)

// FlushFunc flushes buffered log entries.
type FlushFunc func(context.Context) error

// Options defines logger configuration parameters.
type Options struct {
	Service              string
	Version              string
	ProjectID            string
	Environment          string
	StaticLabels         map[string]string
	InstanceID           string
	DisableInstanceID    bool
	Writer               io.Writer
	EnableSourceLocation bool
	LabelNormalizer      func(map[string]string) map[string]string
	Flush                FlushFunc

	extraAllowedKeys map[string]struct{}
	extraLabelKeys   map[string]struct{}
}

// Option customises Options.
type Option func(*Options)

// WithService configures the service name (required).
func WithService(name string) Option {
	return func(o *Options) {
		o.Service = name
	}
}

// WithVersion configures the service version (required).
func WithVersion(version string) Option {
	return func(o *Options) {
		o.Version = version
	}
}

// WithProjectID configures the GCP project used to build trace URLs.
func WithProjectID(project string) Option {
	return func(o *Options) {
		o.ProjectID = project
	}
}

// WithEnvironment configures an environment string (prod/staging/etc).
func WithEnvironment(env string) Option {
	return func(o *Options) {
		o.Environment = env
	}
}

// WithStaticLabels registers constant labels that apply to all entries.
func WithStaticLabels(labels map[string]string) Option {
	return func(o *Options) {
		if len(labels) == 0 {
			return
		}
		if o.StaticLabels == nil {
			o.StaticLabels = make(map[string]string, len(labels))
		}
		for k, v := range labels {
			if k == "" {
				continue
			}
			o.StaticLabels[k] = v
		}
	}
}

// WithInstanceID sets the instance identifier label.
func WithInstanceID(id string) Option {
	return func(o *Options) {
		o.InstanceID = id
	}
}

// DisableInstanceID prevents emitting the instance_id label.
func DisableInstanceID() Option {
	return func(o *Options) {
		o.DisableInstanceID = true
	}
}

// WithWriter configures the output writer (defaults to stdout).
func WithWriter(w io.Writer) Option {
	return func(o *Options) {
		o.Writer = w
	}
}

// EnableSourceLocation records source file / line / function for each entry.
func EnableSourceLocation() Option {
	return func(o *Options) {
		o.EnableSourceLocation = true
	}
}

// WithLabelNormalizer allows overriding label normalisation (e.g. sanitize keys).
func WithLabelNormalizer(fn func(map[string]string) map[string]string) Option {
	return func(o *Options) {
		o.LabelNormalizer = fn
	}
}

// WithFlushFunc overrides the flush behaviour, useful when integrating Cloud Logging client.
func WithFlushFunc(fn FlushFunc) Option {
	return func(o *Options) {
		o.Flush = fn
	}
}

// WithAllowedKeys registers additional key names accepted by the logger.
// Values written with these keys将被合并进 jsonPayload 顶层。
func WithAllowedKeys(keys ...string) Option {
	return func(o *Options) {
		if len(keys) == 0 {
			return
		}
		if o.extraAllowedKeys == nil {
			o.extraAllowedKeys = make(map[string]struct{}, len(keys))
		}
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			o.extraAllowedKeys[key] = struct{}{}
		}
	}
}

// WithAllowedLabelKeys registers additional keys that should be emitted as labels.
func WithAllowedLabelKeys(keys ...string) Option {
	return func(o *Options) {
		if len(keys) == 0 {
			return
		}
		if o.extraLabelKeys == nil {
			o.extraLabelKeys = make(map[string]struct{}, len(keys))
		}
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if o.extraAllowedKeys == nil {
				o.extraAllowedKeys = make(map[string]struct{})
			}
			o.extraAllowedKeys[key] = struct{}{}
			o.extraLabelKeys[key] = struct{}{}
		}
	}
}

// ValidateOptions ensures mandatory fields exist and populates defaults.
func ValidateOptions(opts *Options) error {
	if opts.Service == "" {
		return errors.New("gclog: service is required")
	}
	if opts.Version == "" {
		return errors.New("gclog: version is required")
	}
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	if opts.StaticLabels == nil {
		opts.StaticLabels = make(map[string]string)
	}
	if !opts.DisableInstanceID && opts.InstanceID == "" {
		if host, err := os.Hostname(); err == nil {
			opts.InstanceID = host
		}
	}
	if opts.Flush == nil {
		opts.Flush = func(context.Context) error { return nil }
	}
	return nil
}

// NewLogger constructs a Logger that satisfies Kratos log.Logger.
func NewLogger(opts ...Option) (log.Logger, FlushFunc, error) {
	cfg := &Options{}
	for _, opt := range opts {
		opt(cfg)
	}
	if err := ValidateOptions(cfg); err != nil {
		return nil, nil, err
	}
	staticLabels := make(map[string]string, len(cfg.StaticLabels))
	for k, v := range cfg.StaticLabels {
		staticLabels[k] = v
	}
	labelKeys := make(map[string]struct{}, len(kratosLabelFields)+len(cfg.extraLabelKeys))
	for k := range kratosLabelFields {
		labelKeys[k] = struct{}{}
	}
	for k := range cfg.extraLabelKeys {
		labelKeys[k] = struct{}{}
	}
	payloadKeys := make(map[string]struct{}, len(kratosPayloadFields)+len(cfg.extraAllowedKeys))
	for k := range kratosPayloadFields {
		payloadKeys[k] = struct{}{}
	}
	for k := range cfg.extraAllowedKeys {
		if _, isLabel := labelKeys[k]; isLabel {
			continue
		}
		payloadKeys[k] = struct{}{}
	}
	l := &Logger{
		opts:         *cfg,
		w:            cfg.Writer,
		staticLabels: staticLabels,
		labelKeys:    labelKeys,
		payloadKeys:  payloadKeys,
		allowedKeys: map[string]struct{}{
			log.DefaultMessageKey: {},
			traceKey:              {},
			spanKey:               {},
			callerKey:             {},
			payloadKey:            {},
			labelsKey:             {},
			httpRequestKey:        {},
			errorKey:              {},
		},
	}
	for key := range labelKeys {
		l.allowedKeys[key] = struct{}{}
	}
	for key := range payloadKeys {
		l.allowedKeys[key] = struct{}{}
	}
	return l, cfg.Flush, nil
}

// Logger implements log.Logger and emits Cloud Logging compatible entries.
type Logger struct {
	opts         Options
	w            io.Writer
	mu           sync.Mutex
	staticLabels map[string]string
	allowedKeys  map[string]struct{}
	labelKeys    map[string]struct{}
	payloadKeys  map[string]struct{}
}

// Log implements the Kratos log.Logger interface.
func (l *Logger) Log(level log.Level, keyvals ...interface{}) error {
	if len(keyvals)%2 != 0 {
		keyvals = append(keyvals, nil)
	}

	var (
		msg           string
		traceID       string
		spanID        string
		caller        string
		customPayload map[string]any
		customLabels  map[string]string
		httpReq       *httpRequest
		errValue      string
		extraJSON     map[string]any
	)

	// parse keyvals
	for i := 0; i < len(keyvals); i += 2 {
		key, ok := keyvals[i].(string)
		if !ok {
			continue
		}
		if _, allowed := l.allowedKeys[key]; !allowed {
			return fmt.Errorf("gclog: unsupported log field %q", key)
		}
		val := keyvals[i+1]
		switch key {
		case log.DefaultMessageKey:
			msg, _ = val.(string)
		case traceKey:
			traceID, _ = val.(string)
		case spanKey:
			spanID, _ = val.(string)
		case callerKey:
			caller, _ = val.(string)
		case payloadKey:
			if val == nil {
				continue
			}
			m, ok := val.(map[string]any)
			if !ok {
				return errors.New("gclog: payload must be map[string]any")
			}
			if customPayload == nil {
				customPayload = make(map[string]any, len(m))
			}
			for pk, pv := range m {
				customPayload[pk] = pv
			}
		case labelsKey:
			if customLabels == nil {
				customLabels = make(map[string]string)
			}
			switch v := val.(type) {
			case map[string]string:
				for lk, lv := range v {
					customLabels[lk] = lv
				}
			case map[string]any:
				for lk, lv := range v {
					customLabels[lk] = fmt.Sprint(lv)
				}
			}
		case httpRequestKey:
			switch v := val.(type) {
			case *httpRequest:
				httpReq = v
			case httpRequest:
				h := v
				httpReq = &h
			}
		case errorKey:
			if val != nil {
				errValue = fmt.Sprint(val)
			}
		default:
			if _, ok := l.labelKeys[key]; ok {
				if customLabels == nil {
					customLabels = make(map[string]string)
				}
				customLabels[key] = fmt.Sprint(val)
				continue
			}
			if _, ok := l.payloadKeys[key]; ok {
				if extraJSON == nil {
					extraJSON = make(map[string]any)
				}
				if key == "latency" {
					if secs, ok := val.(float64); ok {
						extraJSON[key] = formatDurationSeconds(secs)
						continue
					}
				}
				extraJSON[key] = val
				continue
			}
			if extraJSON == nil {
				extraJSON = make(map[string]any)
			}
			extraJSON[key] = val
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
	}
	if l.opts.Environment != "" {
		entry.ServiceContext.Environment = l.opts.Environment
	}

	entry.Trace = l.composeTrace(traceID)
	entry.SpanID = spanID

	if l.opts.EnableSourceLocation {
		if src := captureSourceLocation(); src != nil {
			entry.SourceLocation = src
		}
	}

	if httpReq != nil {
		entry.HTTPRequest = httpReq
	}

	labels := l.composeLabels(caller, customLabels)
	if len(labels) > 0 {
		if l.opts.LabelNormalizer != nil {
			labels = l.opts.LabelNormalizer(labels)
		}
		entry.Labels = labels
	}

	var jsonPayload map[string]any
	if len(customPayload) > 0 || errValue != "" || len(extraJSON) > 0 {
		jsonPayload = make(map[string]any, len(customPayload)+len(extraJSON)+2)
		if len(customPayload) > 0 {
			jsonPayload[payloadKey] = customPayload
		}
		if errValue != "" {
			jsonPayload[errorKey] = errValue
		}
		for k, v := range extraJSON {
			jsonPayload[k] = v
		}
	}
	if len(jsonPayload) > 0 {
		entry.JSONPayload = jsonPayload
	}

	return l.write(entry)
}

func (l *Logger) composeTrace(traceID string) string {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return ""
	}
	if strings.HasPrefix(traceID, "projects/") {
		return traceID
	}
	if l.opts.ProjectID != "" {
		return fmt.Sprintf("projects/%s/traces/%s", l.opts.ProjectID, traceID)
	}
	return traceID
}

func (l *Logger) composeLabels(caller string, custom map[string]string) map[string]string {
	labels := make(map[string]string, len(l.staticLabels)+4)
	for k, v := range l.staticLabels {
		labels[k] = v
	}
	if !l.opts.DisableInstanceID && l.opts.InstanceID != "" {
		labels["instance_id"] = l.opts.InstanceID
	}
	if caller != "" {
		labels[callerKey] = caller
	}
	for k, v := range custom {
		if k == "" {
			continue
		}
		labels[k] = v
	}
	return labels
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

func formatDurationSeconds(seconds float64) string {
	if seconds <= 0 {
		return "0s"
	}
	return fmt.Sprintf("%.3fs", seconds)
}

// captureSourceLocation returns caller info when enabled.
func captureSourceLocation() *sourceLocation {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(4, pcs)
	for _, pc := range pcs[:n] {
		fn := runtime.FuncForPC(pc)
		if fn == nil {
			continue
		}
		file, line := fn.FileLine(pc)
		if file == "" || strings.Contains(file, "/runtime/") || strings.Contains(file, "/testing/") {
			continue
		}
		return &sourceLocation{
			File:     file,
			Line:     line,
			Function: fn.Name(),
		}
	}
	return nil
}

// severityFromLevel converts Kratos level to Cloud Logging severity text.
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

// logEntry mirrors Cloud Logging JSON structure.
type logEntry struct {
	Timestamp      string            `json:"timestamp"`
	Severity       string            `json:"severity,omitempty"`
	Message        string            `json:"message,omitempty"`
	ServiceContext serviceContext    `json:"serviceContext"`
	Trace          string            `json:"trace,omitempty"`
	SpanID         string            `json:"spanId,omitempty"`
	SourceLocation *sourceLocation   `json:"sourceLocation,omitempty"`
	HTTPRequest    *httpRequest      `json:"httpRequest,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	JSONPayload    map[string]any    `json:"jsonPayload,omitempty"`
}

type serviceContext struct {
	Service     string `json:"service"`
	Version     string `json:"version"`
	Environment string `json:"environment,omitempty"`
}

type sourceLocation struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Function string `json:"function,omitempty"`
}

type httpRequest struct {
	RequestMethod                  string `json:"requestMethod,omitempty"`
	RequestURL                     string `json:"requestUrl,omitempty"`
	RequestSize                    string `json:"requestSize,omitempty"`
	Status                         int    `json:"status,omitempty"`
	ResponseSize                   string `json:"responseSize,omitempty"`
	UserAgent                      string `json:"userAgent,omitempty"`
	RemoteIP                       string `json:"remoteIp,omitempty"`
	ServerIP                       string `json:"serverIp,omitempty"`
	Referer                        string `json:"referer,omitempty"`
	Latency                        string `json:"latency,omitempty"`
	CacheLookup                    bool   `json:"cacheLookup,omitempty"`
	CacheHit                       bool   `json:"cacheHit,omitempty"`
	CacheValidatedWithOriginServer bool   `json:"cacheValidatedWithOriginServer,omitempty"`
	CacheFillBytes                 string `json:"cacheFillBytes,omitempty"`
	Protocol                       string `json:"protocol,omitempty"`
}
