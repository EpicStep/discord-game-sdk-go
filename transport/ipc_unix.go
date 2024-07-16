//go:build unix

package transport

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

const (
	ipcFileName = "/discord-ipc-0"
)

func openConn(ctx context.Context) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(getIPCPath(), ipcFileName))
}

const (
	defaultPath = "/tmp"
)

var (
	tmpVariables   = []string{"XDG_RUNTIME_DIR", "TMPDIR", "TMP", "TEMP"}
	sandboxPatches = []string{
		"/run/user/1000/snap.discord",
		"/run/user/1000/.flatpak/com.discordapp.Discord/xdg-run",
	}
)

func getIPCPath() string {
	for _, path := range sandboxPatches {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	for _, variable := range tmpVariables {
		if path, exists := os.LookupEnv(variable); exists {
			return path
		}
	}

	return defaultPath
}
