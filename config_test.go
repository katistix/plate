package main

import (
	"fmt"
	"testing"
)

func TestGetDockerRunArgs(t *testing.T) {
	testCases := []struct {
		config        ServiceConfig
		containerName string
		expectedConn  string
		expectedArgs  []string
		expectedErr   error
	}{
		{
			config:        ServiceConfig{Type: "postgres", Version: "14-alpine", Port: 5432},
			containerName: "test-postgres",
			expectedConn:  "postgres://postgres:mysecretpassword@localhost:5432/postgres?sslmode=disable",
			expectedArgs:  []string{"run", "-d", "--name", "test-postgres", "-e", "POSTGRES_PASSWORD=mysecretpassword", "-p", "5432:5432", "postgres:14-alpine"},
			expectedErr:   nil,
		},
		{
			config:        ServiceConfig{Type: "redis", Version: "7", Port: 6379},
			containerName: "test-redis",
			expectedConn:  "redis://localhost:6379",
			expectedArgs:  []string{"run", "-d", "--name", "test-redis", "-p", "6379:6379", "redis:7"},
			expectedErr:   nil,
		},
		{
			config:        ServiceConfig{Type: "mysql", Version: "8", Port: 3306},
			containerName: "test-mysql",
			expectedConn:  "mysql://root:mysecretpassword@localhost:3306/mysql",
			expectedArgs:  []string{"run", "-d", "--name", "test-mysql", "-e", "MYSQL_ROOT_PASSWORD=mysecretpassword", "-p", "3306:3306", "mysql:8"},
			expectedErr:   nil,
		},
		{
			config:        ServiceConfig{Type: "mongodb", Version: "latest", Port: 27017},
			containerName: "test-mongo",
			expectedConn:  "mongodb://localhost:27017",
			expectedArgs:  []string{"run", "-d", "--name", "test-mongo", "-p", "27017:27017", "mongo:latest"},
			expectedErr:   nil,
		},
		{
			config:        ServiceConfig{Type: "unknown", Version: "1.0", Port: 1234},
			containerName: "test-unknown",
			expectedConn:  "",
			expectedArgs:  nil,
			expectedErr:   fmt.Errorf("unknown service type: unknown"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.config.Type, func(t *testing.T) {
			connStr, args, err := getDockerRunArgs(tc.config, tc.containerName)

			if (err != nil && tc.expectedErr == nil) || (err == nil && tc.expectedErr != nil) || (err != nil && tc.expectedErr != nil && err.Error() != tc.expectedErr.Error()) {
				t.Errorf("Expected error '%v', got '%v'", tc.expectedErr, err)
			}

			if connStr != tc.expectedConn {
				t.Errorf("Expected connection string '%s', got '%s'", tc.expectedConn, connStr)
			}

			if len(args) != len(tc.expectedArgs) {
				t.Errorf("Expected args %v, got %v", tc.expectedArgs, args)
			}

			for i, arg := range args {
				if arg != tc.expectedArgs[i] {
					t.Errorf("Expected arg %s, got %s", tc.expectedArgs[i], arg)
				}
			}
		})
	}
}
