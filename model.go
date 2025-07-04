package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// item struct and its methods are unchanged.
type item struct {
	config           ServiceConfig
	status           status
	statusText       string
	connectionString string
	containerID      string
	confirming       confirmationAction
}

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

// --- MAIN MODEL ---
type model struct {
	list        list.Model
	spinner     spinner.Model
	err         error
	quitting    bool
	showCopied  bool
	showingHelp bool // New state for showing the help view
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
		BorderForeground(katistixOrange).
		Foreground(katistixOrange).
		Padding(0, 0, 0, 1)
	delegate.Styles.SelectedTitle = selectedStyle
	delegate.Styles.SelectedDesc = selectedStyle.Copy().Foreground(lipgloss.Color("250")).Faint(true)

	l := list.New(items, delegate, 0, 0)
	l.Title = "Plate Dev Environment"
	l.Styles.Title = titleStyle
	l.SetShowHelp(false)

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
	// If showing help, only listen for keys that hide it.
	if m.showingHelp {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch key.String() {
			case "h", "q", "esc":
				m.showingHelp = false
			}
		}
		return m, nil
	}

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
		// When in confirmation mode, we only want to handle y/n/esc.
		if m.list.SelectedItem() != nil && m.list.SelectedItem().(item).confirming != actionNone {
			selectedItem, _ := m.list.SelectedItem().(item)
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
		// Handle regular key presses.
		switch msg.String() {
		case "h":
			m.showingHelp = true
		case "q", "ctrl+c":
			m.quitting = true
			return m, stopAllContainersOnExit(m.list.Items())
		case "s":
			if selectedItem, ok := m.list.SelectedItem().(item); ok && selectedItem.status == statusRunning {
				m.list.SetItem(m.list.Index(), selectedItem)
				return m, stopContainerCmd(m.list.Index(), selectedItem.containerID)
			}
		case "b":
			if selectedItem, ok := m.list.SelectedItem().(item); ok && selectedItem.status == statusStopped {
				m.list.SetItem(m.list.Index(), selectedItem)
				return m, restartContainerCmd(m.list.Index(), selectedItem.config, selectedItem.containerID)
			}
		case "r":
			if selectedItem, ok := m.list.SelectedItem().(item); ok && selectedItem.containerID != "" {
				selectedItem.confirming = actionReset
				return m, m.list.SetItem(m.list.Index(), selectedItem)
			}
		case "d":
			if selectedItem, ok := m.list.SelectedItem().(item); ok && selectedItem.containerID != "" {
				selectedItem.confirming = actionDelete
				return m, m.list.SetItem(m.list.Index(), selectedItem)
			}
		case "c":
			if selectedItem, ok := m.list.SelectedItem().(item); ok && selectedItem.status == statusRunning && selectedItem.connectionString != "" {
				m.showCopied = true
				return m, tea.Batch(copyToClipboardCmd(selectedItem.connectionString), tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return copiedToClipboardMsg{} }))
			}
		}

	case copiedToClipboardMsg:
		m.showCopied = false
		return m, nil

	// Handle command results
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
		return docStyle.Render(fmt.Sprintf("\n%s Stopping containers... Please wait.\n", m.spinner.View()))
	}
	if m.showingHelp {
		return m.renderFullHelpView()
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

func (m model) renderFullHelpView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Plate Help"))
	b.WriteString("\n\n")
	b.WriteString(detailAttrStyle.Render("Plate is a simple TUI for managing development databases with Docker.\n"))
	b.WriteString("It reads a `plate.config.json` file to provision the services you need.\n\n")

	b.WriteString(detailTitleStyle.Render("In-App Commands") + "\n")
	b.WriteString(fmt.Sprintf("%s: Navigate the list of services.\n", detailAttrStyle.Render("↑/↓")))
	b.WriteString(fmt.Sprintf("%s: Show/hide this help screen.\n", detailAttrStyle.Render("h")))
	b.WriteString(fmt.Sprintf("%s: Stop a running service.\n", detailAttrStyle.Render("s")))
	b.WriteString(fmt.Sprintf("%s: Boot/start a stopped service.\n", detailAttrStyle.Render("b")))
	b.WriteString(fmt.Sprintf("%s: Copy connection string for a running service.\n", detailAttrStyle.Render("c")))
	b.WriteString(fmt.Sprintf("%s: Reset a service (stops, removes, and recreates it).\n", detailAttrStyle.Render("r")))
	b.WriteString(fmt.Sprintf("%s: Delete a service (stops and removes its container).\n", detailAttrStyle.Render("d")))
	b.WriteString(fmt.Sprintf("%s: Quit the application (stops running containers).\n\n", detailAttrStyle.Render("q/ctrl+c")))

	b.WriteString(detailTitleStyle.Render("CLI Commands") + "\n")
	b.WriteString(fmt.Sprintf("%s: Start the TUI.\n", detailAttrStyle.Render("plate")))
	b.WriteString(fmt.Sprintf("%s: Create a default config file.\n", detailAttrStyle.Render("plate init")))
	b.WriteString(fmt.Sprintf("%s: Show command-line help.\n\n", detailAttrStyle.Render("plate help")))

	b.WriteString(helpStyle.Render("Press 'h', 'q', or 'esc' to return."))

	return docStyle.Render(b.String())
}

func (m model) renderHelpView() string {
	helpText := "↑/↓: navigate • h: help • q: quit • s: stop • b: boot • r: reset • d: delete • c: copy"
	return helpStyle.Render("\n" + helpText)
}
