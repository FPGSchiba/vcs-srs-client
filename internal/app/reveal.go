package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// fileManagerBinary returns the absolute path of the file-manager launcher for
// goos, so the executable is never resolved through an untrusted PATH. It is
// pure apart from the injected lookPath, which is only consulted for the
// "other" (xdg-open) case.
//
//   - windows: explorer.exe under the system root (falling back to C:\Windows).
//   - darwin:  /usr/bin/open, a fixed system location.
//   - other:   xdg-open lives in different places per distribution, so it is
//     resolved explicitly and the absolute result is what gets executed.
func fileManagerBinary(goos, systemRoot string, lookPath func(string) (string, error)) (string, error) {
	switch goos {
	case "windows":
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		return filepath.Join(systemRoot, "explorer.exe"), nil
	case "darwin":
		return "/usr/bin/open", nil
	default:
		return lookPath("xdg-open")
	}
}

// openInFileManager reveals dir in the OS file manager. The per-OS switch
// lives here, in one place.
func openInFileManager(dir string) error {
	bin, err := fileManagerBinary(runtime.GOOS, os.Getenv("SYSTEMROOT"), exec.LookPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, dir)
	// Start, not Run: explorer exits non-zero even on success, and the file
	// manager must not block the binding call.
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
