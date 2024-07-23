//go:build windows

package transport

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

func openConn(ctx context.Context, _ net.Dialer, filename string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, `\\.\pipe\`+filename)
}
