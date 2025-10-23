package gcjwt

import "fmt"

// ClientConfig 定义客户端中间件所需配置。
type ClientConfig struct {
	Audience  string `json:"audience" yaml:"audience"`
	Disabled  bool   `json:"disabled" yaml:"disabled"`
	HeaderKey string `json:"header_key,omitempty" yaml:"header_key,omitempty"`
}

// Validate 校验客户端配置。
func (c *ClientConfig) Validate() error {
	if !c.Disabled && c.Audience == "" {
		return fmt.Errorf("client audience is required when middleware is enabled")
	}
	return nil
}

// ServerConfig 定义服务端中间件所需配置。
type ServerConfig struct {
	ExpectedAudience string `json:"expected_audience" yaml:"expected_audience"`
	SkipValidate     bool   `json:"skip_validate" yaml:"skip_validate"`
	Required         bool   `json:"required" yaml:"required"`
	HeaderKey        string `json:"header_key,omitempty" yaml:"header_key,omitempty"`
}

// Validate 校验服务端配置。
func (c *ServerConfig) Validate() error {
	if !c.SkipValidate && c.ExpectedAudience == "" {
		return fmt.Errorf("expected_audience is required when validation is enabled")
	}
	return nil
}
