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
	case "mysql":
		return fmt.Sprintf("mysql://root:mysecretpassword@localhost:%d/mysql", config.Port), nil
	case "mongodb":
		return fmt.Sprintf("mongodb://localhost:%d", config.Port), nil
	default:
		return "", fmt.Errorf("unknown service type: %s", config.Type)
	}
}

// getDockerRunArgs assembles the arguments for the `docker run` command.
func getDockerRunArgs(config ServiceConfig, containerName string) (string, []string, error) {
	connStr, err := getConnectionString(config)
	if err != nil {
		return "", nil, err
	}

	baseArgs := []string{"run", "-d", "--name", containerName}
	var args []string

	switch config.Type {
	case "postgres":
		args = append(baseArgs, "-e", "POSTGRES_PASSWORD=mysecretpassword", "-p", fmt.Sprintf("%d:5432", config.Port), fmt.Sprintf("postgres:%s", config.Version))
	case "redis":
		args = append(baseArgs, "-p", fmt.Sprintf("%d:6379", config.Port), fmt.Sprintf("redis:%s", config.Version))
	case "mysql":
		args = append(baseArgs, "-e", "MYSQL_ROOT_PASSWORD=mysecretpassword", "-p", fmt.Sprintf("%d:3306", config.Port), fmt.Sprintf("mysql:%s", config.Version))
	case "mongodb":
		args = append(baseArgs, "-p", fmt.Sprintf("%d:27017", config.Port), fmt.Sprintf("mongo:%s", config.Version))
	default:
		return "", nil, fmt.Errorf("unknown service type: %s", config.Type)
	}
	return connStr, args, nil
}
