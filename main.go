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
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	statusStopped
	statusRestarting
	statusResetting
	statusDeleting
	statusError
)

func (s status) String() string {
	return [...]string{
		"Pending...", "🔍 Checking...", "📥 Downloading...", "🚀 Starting...", "✅ Running", "🛑 Stopped", "🔄 Restarting...", "💥 Resetting...", "🗑️ Deleting...", "🔥 Error",
	}[s]
}

// Represents a pending destructive action that requires confirmation.
type confirmationAction int

const (
	actionNone confirmationAction = iota
	actionReset
	actionDelete
)

// --- BUBBLE TEA MODEL & ITEMS ---
type item struct {
	config           ServiceConfig
	status           status
	statusText       string // Used for error messages
	connectionString string
	containerID      string
	confirming       confirmationAction // Are we confirming a destructive action?
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
	if i.confirming == actionReset {
		return confirmStyle.Render("Confirm Reset? (y/n)")
	}
	if i.confirming == actionDelete {
		return confirmStyle.Render("Confirm Delete? (y/n)")
	}

	statusStr := i.status.String()
	switch i.status {
	case statusError:
		return errorStyle.Render(fmt.Sprintf("%s: %s", statusStr, i.statusText))
	case statusRunning:
		return successStyle.Render(statusStr)
	case statusDownloading:
		return downloadingStyle.Render(statusStr)
	case statusStopped:
		return stoppedStyle.Render(statusStr)
	default:
		return pendingStyle.Render(statusStr)
	}
}
func (i item) FilterValue() string { return i.config.Name }

// --- BUBBLE TEA MESSAGES ---
type containerStatusMsg struct {
	index       int
	containerID string
	status      string // e.g., "running", "exited", ""
}

type imageStatusMsg struct {
	index    int
	hasImage bool
}

type imagePulledMsg struct {
	index int
	err   error
}

type containerStartedMsg struct {
	index            int
	containerID      string
	connectionString string
	err              error
}

type containerStoppedMsg struct {
	index int
	err   error
}

type containerRemovedMsg struct {
	index   int
	err     error
	isReset bool
}

type copiedToClipboardMsg struct{}

type cleanupCompleteMsg struct{}

// --- MAIN MODEL ---
type model struct {
	list       list.Model
	spinner    spinner.Model
	err        error
	quitting   bool
	showCopied bool // Flag to show "Copied!" message
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
	selectedStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(katistixOrange). // Using Katistix color
		Foreground(katistixOrange).       // Using Katistix color
		Padding(0, 0, 0, 1)

	delegate.Styles.SelectedTitle = selectedStyle
	delegate.Styles.SelectedDesc = selectedStyle.Copy().Foreground(lipgloss.Color("250")).Faint(true)

	l := list.New(items, delegate, 0, 0)
	l.Title = "Plate Dev Environment"
	l.Styles.Title = titleStyle
	l.SetShowHelp(false) // We render our own help, and list wrapping is default.

	s := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("205"))))

	return model{list: l, spinner: s}
}

// --- BUBBLE TEA LOGIC ---
func (m model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.list.Items()))
	for i, itm := range m.list.Items() {
		currentItem := itm.(item)
		currentItem.status = statusChecking
		m.list.SetItem(i, currentItem)
		cmds[i] = checkContainerCmd(i, currentItem.config)
	}
	return tea.Batch(append(cmds, m.spinner.Tick)...)
}

//nolint:cyclop
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.quitting {
		if _, ok := msg.(cleanupCompleteMsg); ok {
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		listWidth := int(float32(msg.Width-h) * 0.45)
		m.list.SetSize(listWidth, msg.Height-v-3)

	case tea.KeyMsg:
		// Don't let the list handle keystrokes when we're in confirmation mode.
		if m.list.SelectedItem().(item).confirming != actionNone {
			selectedItem, ok := m.list.SelectedItem().(item)
			if !ok {
				return m, nil
			}
			selectedIndex := m.list.Index()

			switch msg.String() {
			case "y", "Y":
				switch selectedItem.confirming {
				case actionReset:
					selectedItem.status = statusResetting
					selectedItem.confirming = actionNone
					return m, tea.Batch(m.list.SetItem(selectedIndex, selectedItem), removeContainerCmd(selectedIndex, selectedItem.containerID, true))
				case actionDelete:
					selectedItem.status = statusDeleting
					selectedItem.confirming = actionNone
					return m, tea.Batch(m.list.SetItem(selectedIndex, selectedItem), removeContainerCmd(selectedIndex, selectedItem.containerID, false))
				}
			case "n", "N", "esc":
				selectedItem.confirming = actionNone
				return m, m.list.SetItem(selectedIndex, selectedItem)
			}
			return m, nil
		}

	case copiedToClipboardMsg:
		m.showCopied = false
		return m, nil

	case containerStatusMsg:
		currentItem := m.list.Items()[msg.index].(item)
		switch msg.status {
		case "running":
			currentItem.status = statusRunning
			currentItem.containerID = msg.containerID
			currentItem.connectionString, _ = getConnectionString(currentItem.config)
		case "exited":
			currentItem.status = statusStopped
			currentItem.containerID = msg.containerID
		default:
			currentItem.status = statusChecking
			return m, tea.Batch(m.list.SetItem(msg.index, currentItem), checkImageCmd(msg.index, currentItem.config))
		}
		return m, m.list.SetItem(msg.index, currentItem)

	case imageStatusMsg:
		currentItem := m.list.Items()[msg.index].(item)
		if msg.hasImage {
			currentItem.status = statusStarting
			return m, tea.Batch(m.list.SetItem(msg.index, currentItem), startContainerCmd(msg.index, currentItem.config, ""))
		}
		currentItem.status = statusDownloading
		return m, tea.Batch(m.list.SetItem(msg.index, currentItem), pullImageCmd(msg.index, currentItem.config))

	case imagePulledMsg:
		currentItem := m.list.Items()[msg.index].(item)
		if msg.err != nil {
			currentItem.status = statusError
			currentItem.statusText = msg.err.Error()
		} else {
			currentItem.status = statusStarting
			return m, tea.Batch(m.list.SetItem(msg.index, currentItem), startContainerCmd(msg.index, currentItem.config, ""))
		}
		return m, m.list.SetItem(msg.index, currentItem)

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

	case containerStoppedMsg:
		currentItem := m.list.Items()[msg.index].(item)
		if msg.err != nil {
			currentItem.status = statusError
			currentItem.statusText = msg.err.Error()
		} else {
			currentItem.status = statusStopped
		}
		return m, m.list.SetItem(msg.index, currentItem)

	case containerRemovedMsg:
		currentItem := m.list.Items()[msg.index].(item)
		if msg.err != nil {
			currentItem.status = statusError
			currentItem.statusText = msg.err.Error()
		} else {
			currentItem.containerID = ""
			currentItem.connectionString = ""
			if msg.isReset {
				currentItem.status = statusChecking
				return m, tea.Batch(m.list.SetItem(msg.index, currentItem), checkImageCmd(msg.index, currentItem.config))
			}
			currentItem.status = statusPending
		}
		return m, m.list.SetItem(msg.index, currentItem)
	}

	// Handle key presses and other messages
	var cmd tea.Cmd
	var cmds []tea.Cmd

	// Only handle these key presses if not in confirmation mode
	if m.list.SelectedItem().(item).confirming == actionNone {
		switch keypress := msg.(type) {
		case tea.KeyMsg:
			switch keypress.String() {
			case "ctrl+c", "q":
				m.quitting = true
				return m, stopAllContainersOnExit(m.list.Items())
			case "s":
				selectedItem, ok := m.list.SelectedItem().(item)
				if ok && selectedItem.status == statusRunning {
					selectedIndex := m.list.Index()
					selectedItem.status = statusStopped
					return m, tea.Batch(m.list.SetItem(selectedIndex, selectedItem), stopContainerCmd(selectedIndex, selectedItem.containerID))
				}
			case "b":
				selectedItem, ok := m.list.SelectedItem().(item)
				if ok && selectedItem.status == statusStopped {
					selectedIndex := m.list.Index()
					selectedItem.status = statusRestarting
					return m, tea.Batch(m.list.SetItem(selectedIndex, selectedItem), restartContainerCmd(selectedIndex, selectedItem.config, selectedItem.containerID))
				}
			case "r":
				selectedItem, ok := m.list.SelectedItem().(item)
				if ok && selectedItem.containerID != "" {
					selectedIndex := m.list.Index()
					selectedItem.confirming = actionReset
					return m, m.list.SetItem(selectedIndex, selectedItem)
				}
			case "d":
				selectedItem, ok := m.list.SelectedItem().(item)
				if ok && selectedItem.containerID != "" {
					selectedIndex := m.list.Index()
					selectedItem.confirming = actionDelete
					return m, m.list.SetItem(selectedIndex, selectedItem)
				}
			case "c":
				selectedItem, ok := m.list.SelectedItem().(item)
				if ok && selectedItem.status == statusRunning && selectedItem.connectionString != "" {
					m.showCopied = true
					return m, tea.Batch(copyToClipboardCmd(selectedItem.connectionString), tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
						return copiedToClipboardMsg{}
					}))
				}
			}
		}
	}

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
		return docStyle.Render(fmt.Sprintf("\n%s Stopping containers... Please wait.\n", m.spinner.View()))
	}

	detailView := m.renderDetailView()
	mainView := lipgloss.JoinHorizontal(lipgloss.Top, m.list.View(), detailPaneStyle.Render(detailView))
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

	if selectedItem.status == statusRunning {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("%s: %s\n", detailAttrStyle.Render("Host Port"), detailValStyle.Render(fmt.Sprintf("%d", selectedItem.config.Port))))
		b.WriteString(fmt.Sprintf("%s: %s\n", detailAttrStyle.Render("Container ID"), detailValStyle.Render(selectedItem.containerID[:12])))

		copyStatus := ""
		if m.showCopied {
			copyStatus = " " + copySuccessStyle.Render("Copied!")
		}
		b.WriteString(fmt.Sprintf("%s:%s\n%s\n", detailAttrStyle.Render("Connection URL"), copyStatus, successStyle.Render(selectedItem.connectionString)))
	} else if selectedItem.status == statusStopped {
		b.WriteString(fmt.Sprintf("\n%s: %s\n", detailAttrStyle.Render("Container ID"), detailValStyle.Render(selectedItem.containerID[:12])))
	} else if selectedItem.status == statusError {
		b.WriteString(fmt.Sprintf("\n%s: %s\n", detailAttrStyle.Render("Details"), errorStyle.Render(selectedItem.statusText)))
	} else if selectedItem.confirming != actionNone {
		b.WriteString(fmt.Sprintf("\n%s", confirmStyle.Render("Are you sure? This action cannot be undone.")))
	}

	return b.String()
}

func (m model) renderHelpView() string {
	helpText := "↑/↓: navigate • q: quit • s: stop • b: boot • r: reset • d: delete • c: copy"
	return helpStyle.Render("\n" + helpText)
}

// --- DOCKER & CLIPBOARD COMMANDS ---

func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		clipboard.WriteAll(text)
		return nil // We don't need a message back, the tick will handle UI
	}
}

func checkContainerCmd(index int, config ServiceConfig) tea.Cmd {
	return func() tea.Msg {
		containerName := fmt.Sprintf("plate-%s-%s", config.Type, config.Name)
		cmd := exec.Command("docker", "ps", "-a", "--filter", "name="+containerName, "--format", "{{.ID}}\t{{.State}}")
		output, _ := cmd.CombinedOutput()
		if len(output) > 0 {
			parts := strings.Split(strings.TrimSpace(string(output)), "\t")
			if len(parts) == 2 {
				return containerStatusMsg{index: index, containerID: parts[0], status: parts[1]}
			}
		}
		return containerStatusMsg{index: index, status: "not_found"}
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

func startContainerCmd(index int, config ServiceConfig, containerID string) tea.Cmd {
	return func() tea.Msg {
		containerName := fmt.Sprintf("plate-%s-%s", config.Type, config.Name)
		connStr, runArgs := getDockerRunArgs(config, containerName)
		runCmd := exec.Command("docker", runArgs...)
		output, err := runCmd.CombinedOutput()
		if err != nil {
			return containerStartedMsg{index: index, err: fmt.Errorf(strings.TrimSpace(string(output)))}
		}
		return containerStartedMsg{index: index, containerID: strings.TrimSpace(string(output)), connectionString: connStr}
	}
}

func restartContainerCmd(index int, config ServiceConfig, containerID string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("docker", "start", containerID)
		if err := cmd.Run(); err != nil {
			return containerStartedMsg{index: index, err: err}
		}
		connStr, _ := getConnectionString(config)
		return containerStartedMsg{index: index, containerID: containerID, connectionString: connStr}
	}
}

func stopContainerCmd(index int, containerID string) tea.Cmd {
	return func() tea.Msg {
		err := exec.Command("docker", "stop", containerID).Run()
		return containerStoppedMsg{index: index, err: err}
	}
}

func removeContainerCmd(index int, containerID string, isReset bool) tea.Cmd {
	return func() tea.Msg {
		_ = exec.Command("docker", "stop", containerID).Run()
		err := exec.Command("docker", "rm", containerID).Run()
		return containerRemovedMsg{index: index, err: err, isReset: isReset}
	}
}

func stopAllContainersOnExit(items []list.Item) tea.Cmd {
	return func() tea.Msg {
		var wg sync.WaitGroup
		for _, itm := range items {
			item := itm.(item)
			if item.containerID != "" && item.status == statusRunning {
				wg.Add(1)
				go func(cid string) {
					defer wg.Done()
					exec.Command("docker", "stop", cid).Run()
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
	// Fixing lipgloss import path
	p := tea.NewProgram(initialModel(plateConfig), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
