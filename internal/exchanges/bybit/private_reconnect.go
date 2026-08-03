package bybit

import "context"

// Reconnect closes any stale socket and completes a bounded backfill before
// the engine can consider the private stream healthy again.
func (source *BybitPrivateEventSource) Reconnect(ctx context.Context) error {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	if source.closed {
		return ErrDemoPrivateEvent
	}
	return source.reconnectLocked(ctx)
}

func (source *BybitPrivateEventSource) reconnectLocked(
	ctx context.Context,
) error {
	if source.connection != nil {
		_ = source.connection.Close()
	}
	source.connection = nil
	source.pending = nil
	return source.connectAndBackfillLocked(ctx)
}
