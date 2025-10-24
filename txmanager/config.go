package txmanager

import "time"

const defaultMeterName = "lingo-utils.txmanager"

// Config controls default behaviour of the transaction manager component.
type Config struct {
	DefaultIsolation string        `json:"defaultIsolation" yaml:"defaultIsolation"`
	DefaultTimeout   time.Duration `json:"defaultTimeout" yaml:"defaultTimeout"`
	LockTimeout      time.Duration `json:"lockTimeout" yaml:"lockTimeout"`
	MaxRetries       int           `json:"maxRetries" yaml:"maxRetries"`
	MeterName        string        `json:"meterName" yaml:"meterName"`
	MetricsEnabled   *bool         `json:"metricsEnabled" yaml:"metricsEnabled"`
}

func (c Config) sanitized() Config {
	s := c
	if s.DefaultIsolation == "" {
		s.DefaultIsolation = "read_committed"
	}
	if s.DefaultTimeout <= 0 {
		s.DefaultTimeout = 3 * time.Second
	}
	if s.LockTimeout < 0 {
		s.LockTimeout = 0
	}
	if s.MeterName == "" {
		s.MeterName = defaultMeterName
	}
	if s.MaxRetries < 0 {
		s.MaxRetries = 0
	}
	if s.MetricsEnabled == nil {
		s.MetricsEnabled = boolPtr(true)
	}
	return s
}

func (c Config) metricsEnabledValue() bool {
	if c.MetricsEnabled == nil {
		return true
	}
	return *c.MetricsEnabled
}

// TxOptionPreset groups the most commonly used transaction presets derived from
// configuration defaults.
type TxOptionPreset struct {
	Default      TxOptions
	Serializable TxOptions
	ReadOnly     TxOptions
}

// BuildPresets builds the preset transaction options using the provided
// configuration values.
func (c Config) BuildPresets() TxOptionPreset {
	cfg := c.sanitized()
	defaultOpt := TxOptions{
		Isolation:   parseIsolation(cfg.DefaultIsolation),
		AccessMode:  ReadWrite,
		Timeout:     cfg.DefaultTimeout,
		LockTimeout: cfg.LockTimeout,
	}
	serializable := defaultOpt
	serializable.Isolation = Serializable

	readOnly := defaultOpt
	readOnly.AccessMode = ReadOnly

	return TxOptionPreset{
		Default:      defaultOpt,
		Serializable: serializable,
		ReadOnly:     readOnly,
	}
}

func boolPtr(b bool) *bool {
	v := b
	return &v
}
