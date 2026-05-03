package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Environment string      `yaml:"environment"`
	Neo4j       Neo4jConfig `yaml:"neo4j"`
}

type Neo4jConfig struct {
	URI      string `yaml:"uri"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Load reads the configuration from the specified environment file,
// and optionally overrides values using environment variables.
func Load(env string) (*Config, error) {
	// Attempt to load .env file if it exists, but don't fail if it doesn't
	_ = godotenv.Load()

	filename := fmt.Sprintf("config.%s.yaml", env)
	
	// Default values
	cfg := &Config{
		Environment: env,
		Neo4j: Neo4jConfig{
			URI:      "bolt://localhost:7687",
			Username: "neo4j",
			Password: "password",
		},
	}

	data, err := os.ReadFile(filename)
	if err == nil {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config file %s: %w", filename, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	// Override with environment variables if present
	if uri := os.Getenv("NEO4J_URI"); uri != "" {
		cfg.Neo4j.URI = uri
	}
	if user := os.Getenv("NEO4J_USERNAME"); user != "" {
		cfg.Neo4j.Username = user
	}
	if pass := os.Getenv("NEO4J_PASSWORD"); pass != "" {
		cfg.Neo4j.Password = pass
	}

	return cfg, nil
}
