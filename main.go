package main

import (
	"encoding/json"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// Check for subcommands like 'init' or 'help'
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "init":
			handleInitCmd()
			return
		case "help":
			handleHelpCmd()
			return
		}
	}

	// Default behavior: start the TUI
	configPath := "plate.config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	configFile, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Error: Config file not found.")
			fmt.Println("Run 'plate init' to create a default config file, or 'plate help' for more options.")
			os.Exit(1)
		}
		fmt.Printf("Error: Could not read '%s'. %v\n", configPath, err)
		os.Exit(1)
	}

	var plateConfig PlateConfig
	if err = json.Unmarshal(configFile, &plateConfig); err != nil {
		fmt.Printf("Error: Could not parse '%s'. %v\n", configPath, err)
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(plateConfig), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}

// handleInitCmd creates a boilerplate plate.config.json.
func handleInitCmd() {
	const defaultConfig = `{
		"services": [
				{
						"type": "postgres",
						"name": "main-db",
						"version": "14-alpine",
						"port": 5433
				},
		]
}`
	configPath := "plate.config.json"
	if _, err := os.Stat(configPath); err == nil {
		fmt.Printf("'%s' already exists. Aborting to prevent overwrite.\n", configPath)
		os.Exit(1)
	}

	err := os.WriteFile(configPath, []byte(defaultConfig), 0644)
	if err != nil {
		fmt.Printf("Error writing config file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Created default '%s'.\n", configPath)
}

// handleHelpCmd prints the command-line help text.
func handleHelpCmd() {
	fmt.Println(`Plate - A simple dev environment provisioner.

Usage:
		plate                  - Start the TUI with 'plate.config.json' in the current directory.
		plate [path/to/config] - Start the TUI with a specific config file.
		plate init             - Create a default 'plate.config.json' in the current directory.
		plate help             - Show this help message.

In-App Commands:
		Press 'h' inside the app to see a list of interactive commands.`)
}
