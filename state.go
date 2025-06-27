package main

// --- STATE MANAGEMENT ---

// status represents the lifecycle of a service.
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

// confirmationAction represents a pending destructive action.
type confirmationAction int

const (
	actionNone confirmationAction = iota
	actionReset
	actionDelete
)
