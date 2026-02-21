package config

import (
	"os"

	"go.yaml.in/yaml/v2"
)

type Config struct {
	Development bool `yaml:"development"`

	BackupConfig BackupConfig `yaml:"backupConfig"`
}

type BackupConfig struct {
	Image         string `yaml:"image"`
	SplitSize     string `yaml:"splitSize"`
	DefaultRegion string `yaml:"defaultRegion"`
	IgnoreTls     bool   `yaml:"ignoreTls"`
}

func NewDefaultConfig() *Config {
	return &Config{
		Development: false,
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
