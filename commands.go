package main

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// --- BUBBLE TEA MESSAGES ---
// These messages are the results of commands.

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
			i := itm.(item)
			if i.containerID != "" && i.status == statusRunning {
				wg.Add(1)
				go func(cid string) {
					defer wg.Done()
					exec.Command("docker", "stop", cid).Run()
				}(i.containerID)
			}
		}
		wg.Wait()
		return cleanupCompleteMsg{}
	}
}
