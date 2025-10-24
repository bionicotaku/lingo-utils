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
	MetricsEnabled   bool          `json:"metricsEnabled" yaml:"metricsEnabled"`
}

func (c Config) sanitized() Config {
	if c.DefaultIsolation == "" {
		c.DefaultIsolation = "read_committed"
	}
	if c.DefaultTimeout <= 0 {
		c.DefaultTimeout = 3 * time.Second
	}
	if c.LockTimeout < 0 {
		c.LockTimeout = 0
	}
	if c.MeterName == "" {
		c.MeterName = defaultMeterName
	}
	if c.MaxRetries < 0 {
		c.MaxRetries = 0
	}
	if !c.MetricsEnabled {
		c.MetricsEnabled = true
	}
	return c
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
