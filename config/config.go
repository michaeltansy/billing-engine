package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// Config represents the overall configuration structure.
type Config struct {
	Server   ServerConfig         `yaml:"server"`
	Database DatabaseConfig       `yaml:"database"`
	API      map[string]APIConfig `yaml:"api"`
}

// ServerConfig holds server-related settings.
type ServerConfig struct {
	Port    int `yaml:"port"`
	Timeout int `yaml:"timeout"`
}

// DatabaseConfig holds database connection settings.
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	DbName   string `yaml:"dbname"`
}

// APIConfig holds API settings.
type APIConfig struct {
	ReqTimeoutInMS int `yaml:"req_timeout_in_ms"`
}

// LoadConfig reads a YAML file and unmarshals it into a Config struct.
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal yaml: %w", err)
	}

	return &config, nil
}
