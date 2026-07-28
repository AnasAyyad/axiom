package sandbox

import (
	"context"
	"errors"
	"time"
)

// SandboxDispatcher is intentionally separate from execution.Broker. It owns
// asynchronous durable testnet/demo delivery under an account fencing token.
type SandboxDispatcher struct {
	account    AccountID
	epoch      uint64
	worker     string
	fence      uint64
	repository DispatcherRepository
	broker     OrderBroker
	kill       KillPoint
	claimTTL   time.Duration
}

// NewSandboxDispatcher binds one worker to one account epoch and fencing token.
func NewSandboxDispatcher(
	account AccountID,
	epoch uint64,
	worker string,
	fence uint64,
	repository DispatcherRepository,
	broker OrderBroker,
	kill KillPoint,
) (*SandboxDispatcher, error) {
	if account == "" || epoch == 0 || worker == "" || fence == 0 ||
		repository == nil || broker == nil {
		return nil, contractError("dispatcher_invalid")
	}
	if kill == nil {
		kill = NoKillPoint{}
	}
	return &SandboxDispatcher{
		account: account, epoch: epoch, worker: worker, fence: fence,
		repository: repository, broker: broker, kill: kill, claimTTL: 30 * time.Second,
	}, nil
}

// DispatchOnce claims and attempts a bounded page. Ambiguous transport results
// become UNKNOWN and keep their cap/reservation capacity.
func (dispatcher *SandboxDispatcher) DispatchOnce(
	ctx context.Context,
	now time.Time,
	limit int,
) (int, error) {
	if now.IsZero() || now.Location() != time.UTC || limit < 1 || limit > 32 {
		return 0, contractError("dispatcher_attempt_invalid")
	}
	records, err := dispatcher.repository.ClaimOutbox(
		ctx, dispatcher.account, dispatcher.epoch, dispatcher.worker, dispatcher.fence,
		now, dispatcher.claimTTL, limit, dispatcher.kill,
	)
	if err != nil {
		return 0, err
	}
	completed := 0
	for _, record := range records {
		if err := dispatcher.dispatchRecord(ctx, record, now); err != nil {
			return completed, err
		}
		completed++
	}
	return completed, nil
}

func (dispatcher *SandboxDispatcher) dispatchRecord(
	ctx context.Context,
	record SubmissionOutbox,
	now time.Time,
) error {
	if err := dispatcher.repository.MarkSubmitting(
		ctx, record.ID, dispatcher.fence, now, dispatcher.kill,
	); err != nil {
		return err
	}
	if err := dispatcher.kill.Hit(ctx, KillBeforeNetworkAttempt); err != nil {
		return err
	}
	event, submitErr := dispatcher.broker.Submit(ctx, record.Submission)
	if err := dispatcher.kill.Hit(ctx, KillAfterNetworkAttempt); err != nil {
		return err
	}
	if submitErr != nil {
		return dispatcher.repository.MarkUnknown(
			ctx, record.ID, dispatcher.fence, now, dispatcher.kill,
		)
	}
	if err := dispatcher.kill.Hit(ctx, KillBeforeAcknowledgement); err != nil {
		return err
	}
	if err := dispatcher.repository.AppendPrivateEvent(
		ctx, record.ID, dispatcher.fence, event, dispatcher.kill,
	); err != nil {
		return err
	}
	return dispatcher.kill.Hit(ctx, KillAfterAcknowledgement)
}

// Cancel remains callable without an active arm, healthy public collector, or
// submission-ready engine.
func (dispatcher *SandboxDispatcher) Cancel(
	ctx context.Context,
	clientOrderID string,
	now time.Time,
) error {
	if clientOrderID == "" || now.IsZero() || now.Location() != time.UTC {
		return contractError("cancel_invalid")
	}
	outboxID, err := dispatcher.repository.MarkCancelPending(
		ctx, dispatcher.account, dispatcher.epoch, clientOrderID,
		dispatcher.worker, dispatcher.fence, now, dispatcher.kill,
	)
	if err != nil {
		return err
	}
	event, cancelErr := dispatcher.attemptCancel(ctx, clientOrderID)
	return dispatcher.recordCancelResult(ctx, outboxID, event, cancelErr, now)
}

func (dispatcher *SandboxDispatcher) attemptCancel(
	ctx context.Context,
	clientOrderID string,
) (PrivateEvent, error) {
	if err := dispatcher.kill.Hit(ctx, KillBeforeNetworkAttempt); err != nil {
		return PrivateEvent{}, err
	}
	event, err := dispatcher.broker.Cancel(ctx, dispatcher.account, dispatcher.epoch, clientOrderID)
	if killErr := dispatcher.kill.Hit(ctx, KillAfterNetworkAttempt); killErr != nil {
		return PrivateEvent{}, killErr
	}
	return event, err
}

func (dispatcher *SandboxDispatcher) recordCancelResult(
	ctx context.Context,
	outboxID string,
	event PrivateEvent,
	cancelErr error,
	now time.Time,
) error {
	if cancelErr != nil {
		if errors.Is(cancelErr, ErrInjectedCrash) {
			return cancelErr
		}
		return dispatcher.repository.MarkCancelUnknown(
			ctx, outboxID, dispatcher.fence, now, dispatcher.kill,
		)
	}
	if err := dispatcher.kill.Hit(ctx, KillBeforeAcknowledgement); err != nil {
		return err
	}
	if err := dispatcher.repository.AppendPrivateEvent(
		ctx, outboxID, dispatcher.fence, event, dispatcher.kill,
	); err != nil {
		return err
	}
	return dispatcher.kill.Hit(ctx, KillAfterAcknowledgement)
}

// ErrInjectedCrash is the deterministic C3 kill-point outcome.
var ErrInjectedCrash = errors.New("sandbox_injected_crash")
