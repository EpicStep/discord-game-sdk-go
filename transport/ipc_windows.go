//go:build windows

package transport

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func openConn(ctx context.Context) (net.Conn, error) {
	return winio.DialPipeContext(ctx, `\\.\pipe\discord-ipc-0`)
}
