package config

import (
	"os"
	"time"

	"go.yaml.in/yaml/v2"
)

type Duration struct {
	time.Duration
}

var _ yaml.Unmarshaler = (*Duration)(nil)

func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = dur
	return nil
}

type Config struct {
	Development bool `yaml:"development"`

	BackupConfig BackupConfig `yaml:"backupConfig"`

	DefaultActionTimeout Duration `yaml:"defaultActionTimeout"`
}

type BackupConfig struct {
	Image         string `yaml:"image"`
	SplitSize     string `yaml:"splitSize"`
	DefaultRegion string `yaml:"defaultRegion"`
	IgnoreTls     bool   `yaml:"ignoreTls"`
}

func NewDefaultConfig() *Config {
	return &Config{
		Development:          false,
		DefaultActionTimeout: Duration{5 * time.Minute},
		BackupConfig: BackupConfig{
			Image:         "docker.io/rocketchat/portmaster:latest",
			SplitSize:     "100Mi",
			DefaultRegion: "us-east-1",
			IgnoreTls:     false,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := NewDefaultConfig()

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if err := yaml.NewDecoder(file).Decode(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
