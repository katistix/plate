package main

import "github.com/charmbracelet/lipgloss"

// --- STYLES ---
var (
	// Katistix brand color
	katistixOrange = lipgloss.Color("#ff4f00")

	docStyle   = lipgloss.NewStyle().Margin(1, 2)
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(katistixOrange). // Using Katistix color
			Padding(0, 1)
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// Status styles
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	successStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	pendingStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	downloadingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	stoppedStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	confirmStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	copySuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("46")).Bold(true)

	// Detail view styles
	detailTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FAFAFA")).
				Background(katistixOrange). // Using Katistix color
				Padding(0, 1)
	detailAttrStyle = lipgloss.NewStyle().Bold(true)
	detailValStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	detailPaneStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(katistixOrange) // Using Katistix color
)
