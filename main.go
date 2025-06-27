// main.go
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- STYLES ---
var (
	// Base document style
	docStyle = lipgloss.NewStyle().Margin(1, 2)

	// Title style for the main list
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFDF5")).
			Background(lipgloss.Color("#5A46E0")).
			Padding(0, 1)

	// Style for general help text
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	// Styles for different service statuses
	errorStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	successStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("46"))
	pendingStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	downloadingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")) // Blue for downloading

	// Style for the new detail view pane
	detailTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FAFAFA")).
				Background(lipgloss.Color("#7D56F4")).
				Padding(0, 1)

	detailAttrStyle = lipgloss.NewStyle().Bold(true)
	detailValStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	detailPaneStyle = lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))
)

// --- CONFIGURATION ---
type ServiceConfig struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Port    int    `json:"port"`
}

type PlateConfig struct {
	Services []ServiceConfig `json:"services"`
}

// --- STATE MANAGEMENT ---
type status int

const (
	statusPending status = iota
	statusChecking
	statusDownloading
	statusStarting
	statusRunning
	statusAlreadyRunning
	statusError
)

func (s status) String() string {
	return [...]string{
		"Pending...", "🔍 Checking...", "📥 Downloading...", "🚀 Starting...", "✅ Running", "✅ Already Running", "🔥 Error",
	}[s]
}

// --- BUBBLE TEA MODEL & ITEMS ---
type item struct {
	config           ServiceConfig
	status           status
	statusText       string // Used for error messages
	connectionString string
	containerID      string
}

// Implement list.Item interface
func (i item) Title() string {
	icon := "❓"
	switch i.config.Type {
	case "postgres":
		icon = "🐘"
	case "redis":
		icon = "🟥"
	}
	return fmt.Sprintf("%s %s", icon, i.config.Name)
}

func (i item) Description() string {
	statusStr := i.status.String()
	if i.status == statusError {
		return errorStyle.Render(fmt.Sprintf("%s: %s", statusStr, i.statusText))
	}
	if i.status == statusRunning || i.status == statusAlreadyRunning {
		return successStyle.Render(statusStr)
	}
	if i.status == statusDownloading {
		return downloadingStyle.Render(statusStr)
	}
	return pendingStyle.Render(statusStr)
}
func (i item) FilterValue() string { return i.config.Name }

// --- BUBBLE TEA MESSAGES ---
// These messages drive the state machine for each service.

// Reports the status after checking for an existing container.
type containerStatusMsg struct {
	index       int
	containerID string
	isRunning   bool
}

// Reports the status after checking for a local docker image.
type imageStatusMsg struct {
	index    int
	hasImage bool
}

// Reports the result of a docker pull command.
type imagePulledMsg struct {
	index int
	err   error
}

// Reports the final result of starting a container.
type containerStartedMsg struct {
	index            int
	containerID      string
	connectionString string
	err              error
}

// A message to indicate cleanup is done.
type cleanupCompleteMsg struct{}

// --- MAIN MODEL ---
type model struct {
	list     list.Model
	spinner  spinner.Model
	err      error
	quitting bool // Flag to indicate we're in the process of shutting down
}

func initialModel(cfg PlateConfig) model {
	items := make([]list.Item, len(cfg.Services))
	for i, s := range cfg.Services {
		items[i] = item{
			config: s,
			status: statusPending,
		}
	}

	delegate := list.NewDefaultDelegate()
	// Fine-tune styles for a cleaner look
	delegate.Styles.SelectedTitle = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, false, true).BorderForeground(lipgloss.Color("170")).Foreground(lipgloss.Color("170")).Padding(0, 0, 0, 1)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedTitle.Copy().Foreground(lipgloss.Color("250")).Faint(true)

	l := list.New(items, delegate, 0, 0)
	l.Title = "Plate Dev Environment"
	l.Styles.Title = titleStyle
	l.SetShowHelp(true) // We will manage our own help text.

	s := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("205"))))

	return model{list: l, spinner: s}
}

// --- BUBBLE TEA LOGIC ---
func (m model) Init() tea.Cmd {
	// Start the spinner and begin checking the status of all services concurrently
	cmds := make([]tea.Cmd, len(m.list.Items()))
	for i, itm := range m.list.Items() {
		currentItem := itm.(item)
		currentItem.status = statusChecking
		m.list.SetItem(i, currentItem)
		cmds[i] = checkContainerCmd(i, currentItem.config)
	}
	return tea.Batch(append(cmds, m.spinner.Tick)...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If we're quitting, we wait for the cleanup command to finish
	if m.quitting {
		if _, ok := msg.(cleanupCompleteMsg); ok {
			return m, tea.Quit
		}
		// While quitting, update the spinner
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Adjust layout on window resize
		h, v := docStyle.GetFrameSize()
		// We'll give the list 40% of the width, and the detail view the rest.
		listWidth := int(float32(msg.Width-h) * 0.4)
		m.list.SetSize(listWidth, msg.Height-v)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, stopAllServices(m.list.Items())
		}

	// A container's status has been checked
	case containerStatusMsg:
		currentItem := m.list.Items()[msg.index].(item)
		if msg.isRunning {
			currentItem.status = statusAlreadyRunning
			currentItem.containerID = msg.containerID
			currentItem.connectionString, _ = getConnectionString(currentItem.config)
			return m, m.list.SetItem(msg.index, currentItem)
		}
		// Not running, so now we check for the image
		currentItem.status = statusChecking
		return m, tea.Batch(m.list.SetItem(msg.index, currentItem), checkImageCmd(msg.index, currentItem.config))

	// An image's status has been checked
	case imageStatusMsg:
		currentItem := m.list.Items()[msg.index].(item)
		if msg.hasImage {
			// Image exists, so we can start it
			currentItem.status = statusStarting
			return m, tea.Batch(m.list.SetItem(msg.index, currentItem), startContainerCmd(msg.index, currentItem.config))
		}
		// Image doesn't exist, we need to pull it
		currentItem.status = statusDownloading
		return m, tea.Batch(m.list.SetItem(msg.index, currentItem), pullImageCmd(msg.index, currentItem.config))

	// An image has finished pulling
	case imagePulledMsg:
		currentItem := m.list.Items()[msg.index].(item)
		if msg.err != nil {
			currentItem.status = statusError
			currentItem.statusText = msg.err.Error()
			return m, m.list.SetItem(msg.index, currentItem)
		}
		// Image pulled successfully, now we can start it
		currentItem.status = statusStarting
		return m, tea.Batch(m.list.SetItem(msg.index, currentItem), startContainerCmd(msg.index, currentItem.config))

	// A container has finished starting
	case containerStartedMsg:
		currentItem := m.list.Items()[msg.index].(item)
		if msg.err != nil {
			currentItem.status = statusError
			currentItem.statusText = msg.err.Error()
		} else {
			currentItem.status = statusRunning
			currentItem.containerID = msg.containerID
			currentItem.connectionString = msg.connectionString
		}
		return m, m.list.SetItem(msg.index, currentItem)
	}

	// Handle other messages (like spinner ticks and list navigation)
	var cmd tea.Cmd
	var cmds []tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	cmds = append(cmds, cmd)
	m.list, cmd = m.list.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.err != nil {
		return docStyle.Render(errorStyle.Render(fmt.Sprintf("Fatal error: %v", m.err)))
	}
	if m.quitting {
		return docStyle.Render(fmt.Sprintf("\n%s Cleaning up containers... Please wait.\n", m.spinner.View()))
	}

	// Build the detail view pane
	detailView := m.renderDetailView()

	// Build the main layout
	mainView := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.list.View(),
		detailPaneStyle.Render(detailView),
	)

	// Build the help text view
	helpView := m.renderHelpView()

	return docStyle.Render(lipgloss.JoinVertical(lipgloss.Left, mainView, helpView))
}

func (m model) renderDetailView() string {
	selectedItem, ok := m.list.SelectedItem().(item)
	if !ok {
		return "Select a service to see details."
	}

	var b strings.Builder

	b.WriteString(detailTitleStyle.Render(selectedItem.Title()))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("%s: %s\n", detailAttrStyle.Render("Type"), detailValStyle.Render(selectedItem.config.Type)))
	b.WriteString(fmt.Sprintf("%s: %s\n", detailAttrStyle.Render("Version"), detailValStyle.Render(selectedItem.config.Version)))
	b.WriteString(fmt.Sprintf("%s: %s\n", detailAttrStyle.Render("Status"), selectedItem.Description()))

	if selectedItem.status == statusRunning || selectedItem.status == statusAlreadyRunning {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%s: %s\n", detailAttrStyle.Render("Host Port"), detailValStyle.Render(fmt.Sprintf("%d", selectedItem.config.Port))))
		b.WriteString(fmt.Sprintf("%s: %s\n", detailAttrStyle.Render("Container ID"), detailValStyle.Render(selectedItem.containerID[:12]))) // Short ID
		b.WriteString(fmt.Sprintf("%s:\n%s\n", detailAttrStyle.Render("Connection URL"), successStyle.Render(selectedItem.connectionString)))
	} else if selectedItem.status == statusError {
		b.WriteString(fmt.Sprintf("%s: %s\n", detailAttrStyle.Render("Details"), errorStyle.Render(selectedItem.statusText)))
	}

	return b.String()
}

func (m model) renderHelpView() string {
	allDone := true
	for _, itm := range m.list.Items() {
		if i := itm.(item); i.status < statusRunning { // Any state before Running/Error is not "done"
			allDone = false
			break
		}
	}

	var builder strings.Builder
	if allDone {
		builder.WriteString(helpStyle.Render("↑/↓: navigate • q: quit & clean up"))
	} else {
		builder.WriteString(helpStyle.Render(fmt.Sprintf("%s Processing... • ↑/↓: navigate • q: quit & clean up", m.spinner.View())))
	}
	return builder.String()
}

// --- DOCKER COMMANDS ---

func checkContainerCmd(index int, config ServiceConfig) tea.Cmd {
	return func() tea.Msg {
		containerName := fmt.Sprintf("plate-%s-%s", config.Type, config.Name)
		cmd := exec.Command("docker", "ps", "-a", "--filter", "name="+containerName, "--format", "{{.ID}}\t{{.State}}")
		output, _ := cmd.CombinedOutput()

		if len(output) > 0 {
			parts := strings.Split(strings.TrimSpace(string(output)), "\t")
			if len(parts) == 2 {
				// Container exists. Is it running?
				if parts[1] == "running" {
					return containerStatusMsg{index: index, containerID: parts[0], isRunning: true}
				}
				// It exists but is stopped, remove it before proceeding.
				exec.Command("docker", "rm", "-f", containerName).Run()
			}
		}
		// Container does not exist.
		return containerStatusMsg{index: index, isRunning: false}
	}
}

func checkImageCmd(index int, config ServiceConfig) tea.Cmd {
	return func() tea.Msg {
		imageName := fmt.Sprintf("%s:%s", config.Type, config.Version)
		cmd := exec.Command("docker", "images", "-q", imageName)
		output, _ := cmd.CombinedOutput()
		return imageStatusMsg{index: index, hasImage: len(output) > 0}
	}
}

func pullImageCmd(index int, config ServiceConfig) tea.Cmd {
	return func() tea.Msg {
		imageName := fmt.Sprintf("%s:%s", config.Type, config.Version)
		err := exec.Command("docker", "pull", imageName).Run()
		return imagePulledMsg{index: index, err: err}
	}
}

func startContainerCmd(index int, config ServiceConfig) tea.Cmd {
	return func() tea.Msg {
		containerName := fmt.Sprintf("plate-%s-%s", config.Type, config.Name)
		connStr, runArgs := getDockerRunArgs(config, containerName)

		// We use CombinedOutput() here to capture both stdout and stderr for better error reporting.
		runCmd := exec.Command("docker", runArgs...)
		output, err := runCmd.CombinedOutput()

		if err != nil {
			return containerStartedMsg{index: index, err: fmt.Errorf(strings.TrimSpace(string(output)))}
		}
		return containerStartedMsg{
			index:            index,
			containerID:      strings.TrimSpace(string(output)),
			connectionString: connStr,
		}
	}
}

func stopAllServices(items []list.Item) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		for _, itm := range items {
			item := itm.(item)
			// We stop containers that have a container ID, regardless of their final state
			// This is safer in case a container was started but an error occurred later.
			if item.containerID != "" {
				wg.Add(1)
				go func(cid string) {
					defer wg.Done()
					exec.Command("docker", "stop", cid).Run()
					// The container will be removed automatically because we use the `--rm` flag in `startContainerCmd`
				}(item.containerID)
			}
		}
		wg.Wait()
		return cleanupCompleteMsg{}
	}
}

// --- HELPER FUNCTIONS ---
func getConnectionString(config ServiceConfig) (string, error) {
	switch config.Type {
	case "postgres":
		return fmt.Sprintf("postgres://postgres:mysecretpassword@localhost:%d/postgres?sslmode=disable", config.Port), nil
	case "redis":
		return fmt.Sprintf("redis://localhost:%d", config.Port), nil
	default:
		return "", fmt.Errorf("unknown service type")
	}
}

func getDockerRunArgs(config ServiceConfig, containerName string) (string, []string) {
	connStr, _ := getConnectionString(config)
	// Using --rm ensures containers are cleaned up when they are stopped.
	baseArgs := []string{"run", "-d", "--rm", "--name", containerName}

	switch config.Type {
	case "postgres":
		args := append(baseArgs,
			"-e", "POSTGRES_PASSWORD=mysecretpassword",
			"-p", fmt.Sprintf("%d:5432", config.Port),
			fmt.Sprintf("postgres:%s", config.Version),
		)
		return connStr, args
	case "redis":
		args := append(baseArgs,
			"-p", fmt.Sprintf("%d:6379", config.Port),
			fmt.Sprintf("redis:%s", config.Version),
		)
		return connStr, args
	}
	return "", nil
}

// --- MAIN ---
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
