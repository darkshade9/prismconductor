package eventbus

// LogPayload is the persisted shape for the events table.
type LogPayload struct {
	Type    EventType `json:"type"`
	Payload any       `json:"payload"`
}
