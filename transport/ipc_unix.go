//go:build unix

package transport

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

func openConn(ctx context.Context, dialer net.Dialer, filename string) (net.Conn, error) {
	return dialer.DialContext(
		ctx,
		"unix",
		filepath.Join(getIPCPath(), filename),
	)
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
