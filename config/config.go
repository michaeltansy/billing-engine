package config

import (
	"fmt"
	"os"
	"strconv"

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

// ConfigPath returns the config file to load: $CONFIG_PATH when set, otherwise
// the in-repo default. Containers set CONFIG_PATH; local runs need no env at all.
func ConfigPath() string {
	if p := os.Getenv("CONFIG_PATH"); p != "" {
		return p
	}
	return "files/config/app/config.yaml"
}

// LoadConfig reads a YAML file and unmarshals it into a Config struct, then
// applies environment overrides.
//
// The YAML holds the local-development defaults; the environment holds whatever
// the deployment differs on. That split is what lets one image run unchanged in
// Compose (where the database answers to `db`) and on a laptop (`localhost`)
// without a bind mount rewriting the file underneath it.
func LoadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal yaml: %w", err)
	}

	if err := config.applyEnvOverrides(); err != nil {
		return nil, err
	}

	return &config, nil
}

func (c *Config) applyEnvOverrides() error {
	overrideString("DB_HOST", &c.Database.Host)
	overrideString("DB_USER", &c.Database.Username)
	overrideString("DB_PASSWORD", &c.Database.Password)
	overrideString("DB_NAME", &c.Database.DbName)

	if err := overrideInt("DB_PORT", &c.Database.Port); err != nil {
		return err
	}
	return overrideInt("SERVER_PORT", &c.Server.Port)
}

func overrideString(key string, dst *string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

func overrideInt(key string, dst *int) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("invalid %s %q: %w", key, v, err)
	}

	*dst = n
	return nil
}
