// Package proto implements Discord protocol.
package proto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/EpicStep/discord-game-sdk-go/transport"
)

// Conn implements connect to Discord protocol.
type Conn struct {
	clientID string
	handler  Handler

	conn  transport.Conn
	nonce atomic.Uint64

	dialTimeout      time.Duration
	handshakeTimeout time.Duration
	pingTimeout      time.Duration
	pingInterval     time.Duration

	pongChan    chan struct{}
	pongChanMux sync.Mutex

	rpcCalls    map[string]chan framePacket
	rpcCallsMux sync.Mutex

	run atomic.Bool
}

// Options ...
type Options struct {
	ClientID         string
	Handler          Handler
	DialTimeout      time.Duration
	HandshakeTimeout time.Duration

	PingTimeout  time.Duration
	PingInterval time.Duration
}

func (o *Options) setDefaults() {
	if o.Handler == nil {
		o.Handler = newNoopHandler()
	}

	if o.DialTimeout <= 0 {
		o.DialTimeout = time.Second * 5
	}

	if o.HandshakeTimeout <= 0 {
		o.HandshakeTimeout = time.Second * 10
	}

	if o.PingTimeout <= 0 {
		o.PingTimeout = time.Second * 10
	}

	if o.PingInterval <= 0 {
		o.PingInterval = time.Minute
	}
}

// New returns new Conn.
func New(opts Options) *Conn {
	opts.setDefaults()

	return &Conn{
		clientID: opts.ClientID,
		handler:  opts.Handler,

		handshakeTimeout: opts.HandshakeTimeout,
		dialTimeout:      opts.DialTimeout,
		pingTimeout:      opts.PingTimeout,
		pingInterval:     opts.PingInterval,

		rpcCalls: make(map[string]chan framePacket),
	}
}

var errAlreadyRunning = errors.New("already running")

// Run ...
func (c *Conn) Run(ctx context.Context, f func(ctx context.Context) error) error {
	if c.run.Swap(true) {
		return errAlreadyRunning
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, c.dialTimeout)
	tConn, err := transport.New(dialCtx, transport.Options{})
	dialCancel()
	if err != nil {
		return err
	}

	defer func() {
		_ = tConn.Close() //nolint:errcheck
	}()

	c.conn = tConn

	if err = c.handshake(ctx); err != nil {
		return fmt.Errorf("failed to handshake: %w", err)
	}

	{
		// run background work
		eg, eCtx := errgroup.WithContext(ctx)

		// TODO: wait RPC done before shutdown.

		eg.Go(func() error {
			// handle close
			<-eCtx.Done()
			return c.conn.Close()
		})

		eg.Go(func() error {
			return c.readLoop(eCtx)
		})

		eg.Go(func() error {
			return c.pingLoop(eCtx)
		})

		eg.Go(func() error {
			return f(eCtx)
		})

		if err = eg.Wait(); err != nil {
			return err
		}
	}

	return nil
}

// Invoke RPC method.
func (c *Conn) Invoke(ctx context.Context, command string, args []byte) ([]byte, error) {
	nonce := c.nextNonce()

	input := framePacket{
		Command: command,
		Args:    args,
		Nonce:   nonce,
	}

	packet, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal frame: %w", err)
	}

	frameCh := make(chan framePacket)

	c.rpcCallsMux.Lock()
	c.rpcCalls[nonce] = frameCh
	c.rpcCallsMux.Unlock()

	defer func() {
		c.rpcCallsMux.Lock()
		delete(c.rpcCalls, nonce)
		c.rpcCallsMux.Unlock()

		close(frameCh)
	}()

	if err = c.conn.Write(ctx, frameOpcode, packet); err != nil {
		return nil, fmt.Errorf("failed to write frame: %w", err)
	}

	var resultFrame framePacket

	select {
	// TODO: return when main ctx is closed.
	case <-ctx.Done():
		return nil, ctx.Err()
	case resultFrame = <-frameCh:
	}

	if resultFrame.Event == EventTypeError {
		var e Error

		if err = json.Unmarshal(resultFrame.Data, &e); err != nil {
			return nil, fmt.Errorf("failed to unmarshal error: %w", err)
		}

		return nil, e
	}

	return resultFrame.Data, nil
}

func (c *Conn) handshake(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.handshakeTimeout)
	defer cancel()

	handshakeData, err := json.Marshal(
		handshakePacket{
			Version:  defaultRPCVersion,
			ClientID: c.clientID,
		},
	)
	if err != nil {
		return err
	}

	if err = c.conn.Write(ctx, handshakeOpcode, handshakeData); err != nil {
		return err
	}

	opcode, data, err := c.conn.Read(ctx)
	if err != nil {
		return err
	}

	if opcode != frameOpcode {
		if opcode == closeOpcode {
			var closeError Error

			if err = json.Unmarshal(data, &closeError); err != nil {
				return err
			}

			return closeError
		}

		return fmt.Errorf("unexpected opcode %d", opcode)
	}

	var frame framePacket

	if err = json.Unmarshal(data, &frame); err != nil {
		return err
	}

	if frame.Event != "" {
		c.handler.OnEvent(frame.Event, data)
	}

	if frame.Event != EventTypeReady {
		return fmt.Errorf("unexpected event: %s", frame.Event)
	}

	return nil
}

var errUnexpectedPacket = errors.New("unexpected packet")

func (c *Conn) readLoop(ctx context.Context) error {
	for {
		opcode, data, err := c.conn.Read(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				if noUpdates(err) {
					continue
				}
			}

			return err
		}

		switch opcode {
		case handshakeOpcode:
			// server can't send handshake to us.
			return errUnexpectedPacket
		case frameOpcode:
			if err = c.processFrame(data); err != nil {
				return err
			}
		case closeOpcode:
			var closeError Error

			if err = json.Unmarshal(data, &closeError); err != nil {
				return err
			}

			return closeError
		case pingOpcode:
			if err = c.conn.Write(ctx, pongOpcode, data); err != nil {
				return err
			}
		case pongOpcode:
			c.processPong()
		}
	}
}

func (c *Conn) processFrame(data []byte) error {
	var frame framePacket

	err := json.Unmarshal(data, &frame)
	if err != nil {
		return fmt.Errorf("failed to decode frame: %w", err)
	}

	if frame.Event != "" {
		c.handler.OnEvent(frame.Event, frame.Data)
	}

	if frame.Nonce == "" {
		return nil
	}

	c.rpcCallsMux.Lock()
	defer c.rpcCallsMux.Unlock()
	if ch, ok := c.rpcCalls[frame.Nonce]; ok {
		ch <- frame
	}

	return nil
}

func (c *Conn) pingLoop(ctx context.Context) error {
	ticker := time.NewTicker(c.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := c.ping(ctx); err != nil {
				return fmt.Errorf("ping failed: %w", err)
			}
		}
	}
}

func (c *Conn) ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.pingTimeout)
	defer cancel()

	pongChan := make(chan struct{})

	c.pongChanMux.Lock()
	c.pongChan = pongChan
	c.pongChanMux.Unlock()

	defer func() {
		c.pongChanMux.Lock()
		defer c.pongChanMux.Unlock()

		c.pongChan = nil
	}()

	if err := c.conn.Write(ctx, pingOpcode, []byte("{}")); err != nil {
		return fmt.Errorf("failed to write ping: %w", err)
	}

	select {
	case <-pongChan:
		break
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (c *Conn) processPong() {
	c.pongChanMux.Lock()
	defer c.pongChanMux.Unlock()

	if c.pongChan == nil {
		return
	}

	close(c.pongChan)
}

func (c *Conn) nextNonce() string {
	return strconv.FormatUint(c.nonce.Inc(), 10)
}

func noUpdates(err error) bool {
	var syscall *net.OpError
	if errors.As(err, &syscall) && syscall.Timeout() {
		return true
	}
	return false
}
