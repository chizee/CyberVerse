package orchestrator

import "errors"

var (
	ErrMaxSessions    = errors.New("maximum concurrent sessions reached")
	ErrSessionExists  = errors.New("session already exists")
	ErrSessionNotFound = errors.New("session not found")
)
