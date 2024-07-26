package proto

import (
	"encoding/json"
)

const defaultRPCVersion = "1"

const (
	handshakeOpcode uint32 = iota
	frameOpcode
	closeOpcode
	pingOpcode
	pongOpcode
)

// EventType defines event types (https://discord.com/developers/docs/topics/rpc#commands-and-events).
type EventType string

const (
	// EventTypeReady ...
	EventTypeReady EventType = "READY"
	// EventTypeError ...
	EventTypeError EventType = "ERROR"
)

type handshakePacket struct {
	Version  string `json:"v"`
	ClientID string `json:"client_id"`
}

type framePacket struct {
	Command string          `json:"cmd"`
	Data    json.RawMessage `json:"data"`
	Args    json.RawMessage `json:"args"`
	Event   EventType       `json:"evt"`
	Nonce   string          `json:"nonce"`
}
