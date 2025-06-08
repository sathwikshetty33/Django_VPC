package types

// LogMessage represents a log entry with metadata
type LogMessage struct {
	Level     string `json:"level"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Step      string `json:"step,omitempty"`
}

// LogBroadcaster interface for broadcasting log messages
type LogBroadcaster interface {
	BroadcastLog(deploymentID string, logMsg LogMessage)
}