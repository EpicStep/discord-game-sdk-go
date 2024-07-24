// Package transport defines and implementing Conn to Discord.
package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// Conn to discord.
type Conn interface {
	Write(ctx context.Context, opcode uint32, data []byte) error
	Read(ctx context.Context) (opcode uint32, data []byte, err error)
	Close() error
}

type conn struct {
	conn net.Conn

	writeMux sync.Mutex
	readMux  sync.Mutex
}

// Options ...
type Options struct {
	// Dialer used to connect to IPC (only on unix),
	Dialer net.Dialer
	// InstanceID is variable that you can use to handle multiple Discord clients.
	// Alternative to DISCORD_INSTANCE_ID.
	// https://discord.com/developers/docs/game-sdk/getting-started#testing-locally-with-two-clients-environment-variable-example
	InstanceID uint
}

// New opens new IPC Conn and returns it.
func New(ctx context.Context, opts Options) (Conn, error) {
	netConn, err := openConn(ctx, opts.Dialer, getDiscordFilename(opts.InstanceID))
	if err != nil {
		return nil, err
	}

	return &conn{
		conn: netConn,
	}, nil
}

const (
	// maxMessageSize is a libuv max message size that used as limitation.
	// https://github.com/discord/discord-rpc/blob/master/src/rpc_connection.h#L8
	maxMessageSize = 64 * 1024
)

var errMessageTooBig = errors.New("message too big, max size is " + strconv.Itoa(maxMessageSize))

func (c *conn) Write(ctx context.Context, opcode uint32, data []byte) error {
	// if len of data + opcode + data size > maxMessageSize we need to skip this.
	if len(data)+8 > maxMessageSize {
		return fmt.Errorf("failed to write message: %w", errMessageTooBig)
	}

	c.writeMux.Lock()
	defer c.writeMux.Unlock()

	if err := c.conn.SetWriteDeadline(time.Time{}); err != nil {
		return fmt.Errorf("failed to reset write deadline: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return fmt.Errorf("failed to set write deadline: %w", err)
		}
	}

	// we need to write opcode (uint32) and data size (uint32) into buf.
	writeBuf := make([]byte, 0, 4+4+len(data))

	writeBuf = binary.LittleEndian.AppendUint32(writeBuf, opcode)
	writeBuf = binary.LittleEndian.AppendUint32(writeBuf, uint32(len(data)))
	writeBuf = append(writeBuf, data...)

	if _, err := c.conn.Write(writeBuf); err != nil {
		return fmt.Errorf("failed to write data into conn: %w", err)
	}

	return nil
}

func (c *conn) Read(ctx context.Context) (opcode uint32, data []byte, err error) {
	c.readMux.Lock()
	defer c.readMux.Unlock()

	if err = c.conn.SetReadDeadline(time.Time{}); err != nil {
		return 0, nil, fmt.Errorf("failed to reset read deadline: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err = c.conn.SetReadDeadline(deadline); err != nil {
			return 0, nil, fmt.Errorf("failed to set write deadline: %w", err)
		}
	}

	// we need to read opcode (uint32) and data size (uint32).
	header := make([]byte, 4+4)
	if _, err = io.ReadFull(c.conn, header); err != nil {
		return 0, nil, fmt.Errorf("failed to read header: %w", err)
	}

	opcode = binary.LittleEndian.Uint32(header[:4])
	payloadLength := binary.LittleEndian.Uint32(header[4:8])

	data = make([]byte, payloadLength)
	if _, err = io.ReadFull(c.conn, data); err != nil {
		return 0, nil, fmt.Errorf("failed to read payload: %w", err)
	}

	return opcode, data, nil
}

func (c *conn) Close() error {
	return c.conn.Close()
}

func getDiscordFilename(instanceID uint) string {
	return "discord-ipc-" + strconv.FormatUint(uint64(instanceID), 10)
}
