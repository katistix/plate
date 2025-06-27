package main

import "fmt"

// --- CONFIGURATION ---

// ServiceConfig defines the structure for a single service in the config file.
type ServiceConfig struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Port    int    `json:"port"`
}

// PlateConfig defines the top-level structure of the config file.
type PlateConfig struct {
	Services []ServiceConfig `json:"services"`
}

// --- HELPER FUNCTIONS ---

// getConnectionString constructs the database connection URL.
func getConnectionString(config ServiceConfig) (string, error) {
	switch config.Type {
	case "postgres":
		return fmt.Sprintf("postgres://postgres:mysecretpassword@localhost:%d/postgres?sslmode=disable", config.Port), nil
	case "redis":
		return fmt.Sprintf("redis://localhost:%d", config.Port), nil
	default:
		return "", fmt.Errorf("unknown service type: %s", config.Type)
	}
}

// getDockerRunArgs assembles the arguments for the `docker run` command.
func getDockerRunArgs(config ServiceConfig, containerName string) (string, []string) {
	connStr, _ := getConnectionString(config)
	baseArgs := []string{"run", "-d", "--name", containerName}
	switch config.Type {
	case "postgres":
		args := append(baseArgs, "-e", "POSTGRES_PASSWORD=mysecretpassword", "-p", fmt.Sprintf("%d:5432", config.Port), fmt.Sprintf("postgres:%s", config.Version))
		return connStr, args
	case "redis":
		args := append(baseArgs, "-p", fmt.Sprintf("%d:6379", config.Port), fmt.Sprintf("redis:%s", config.Version))
		return connStr, args
	}
	return "", nil
}
