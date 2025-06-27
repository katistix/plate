package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	configPath := "plate.config.json"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	configFile, err := ioutil.ReadFile(configPath)
	if err != nil {
		fmt.Printf("Error: Could not find or read '%s'. %v\n", configPath, err)
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
