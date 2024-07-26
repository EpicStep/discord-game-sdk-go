package proto

import "strconv"

// Error is a error that return Discord.
type Error struct {
	Code    int    `json:"code"` // TODO: define codes
	Message string `json:"message"`
}

func (e Error) Error() string {
	return "discord code " + strconv.Itoa(e.Code) + ": " + e.Message
}
