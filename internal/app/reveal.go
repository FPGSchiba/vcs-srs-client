package app

import (
	"os/exec"
	"runtime"
)

// openInFileManager reveals dir in the OS file manager. The per-OS switch
// lives here, in one place.
func openInFileManager(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	// Start, not Run: explorer exits non-zero even on success, and the file
	// manager must not block the binding call.
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
