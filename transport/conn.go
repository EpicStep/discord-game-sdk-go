package transport

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type Conn struct {
	conn net.Conn

	writeMux sync.Mutex
	readMux  sync.Mutex
}

func New(ctx context.Context) (*Conn, error) {
	conn, err := openConn(ctx)
	if err != nil {
		return nil, err
	}

	return &Conn{
		conn: conn,
	}, nil
}

func (c *Conn) Write(ctx context.Context, opcode uint32, data []byte) error {
	// TODO: check data size.

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

func (c *Conn) Read(ctx context.Context) (opcode uint32, data []byte, err error) {
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

	// TODO: check data size.

	opcode = binary.LittleEndian.Uint32(header[:4])
	payloadLength := binary.LittleEndian.Uint32(header[4:8])

	data = make([]byte, payloadLength)
	if _, err = io.ReadFull(c.conn, data); err != nil {
		return 0, nil, fmt.Errorf("failed to read payload: %w", err)
	}

	return opcode, data, nil
}

func (c *Conn) Close() error {
	return c.conn.Close()
}
