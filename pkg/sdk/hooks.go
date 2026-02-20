package sdk

import "context"

// Hooks allows callers to observe session lifecycle and persistence behavior.
type Hooks struct {
	OnSessionStart func(ctx context.Context, sessionID string, req ExecuteRequest)
	OnEventStored  func(ctx context.Context, evt Event)
	OnSessionEnd   func(ctx context.Context, sessionID string)
	OnStoreError   func(ctx context.Context, sessionID string, evt Event, err error)
}
