package platform

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level      string            `mapstructure:"level"`
	Format     string            `mapstructure:"format"`
	Console    bool              `mapstructure:"console"`
	File       *FileLogConfig    `mapstructure:"file,omitempty"`
	Components map[string]string `mapstructure:"components,omitempty"`
}
