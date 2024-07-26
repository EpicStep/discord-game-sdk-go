package proto

// Handler ...
type Handler interface {
	OnEvent(eventType EventType, data []byte)
}

type noopHandler struct{}

func newNoopHandler() *noopHandler {
	return &noopHandler{}
}

func (noopHandler) OnEvent(_ EventType, _ []byte) {}
