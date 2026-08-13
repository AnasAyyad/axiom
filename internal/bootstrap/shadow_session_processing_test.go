package bootstrap

import (
	"context"
	"sync"
	"testing"
	"time"

	exchangecontracts "axiom/internal/exchanges/contracts"
	"axiom/internal/marketdata"
)

func TestPublicShadowEvaluationIsTriggeredByCollectorMarketUpdate(t *testing.T) {
	collector := &eventDrivenShadowCollector{updates: make(chan struct{}, 1), running: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	evaluated := make(chan struct{}, 1)
	go func() {
		done <- runPublicShadowCollectors(ctx, []shadowPublicCollector{collector}, time.Hour,
			func(context.Context) error { return nil },
			func(context.Context) error {
				select {
				case evaluated <- struct{}{}:
				default:
				}
				return nil
			},
			func(context.Context) error { return nil })
	}()
	select {
	case <-collector.running:
	case <-time.After(time.Second):
		t.Fatal("collector did not start")
	}
	collector.updates <- struct{}{}
	select {
	case <-evaluated:
	case <-time.After(400 * time.Millisecond):
		t.Fatal("collector event did not trigger evaluation before the 500ms fallback tick")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("public shadow loop did not stop")
	}
}

type eventDrivenShadowCollector struct {
	updates chan struct{}
	running chan struct{}
	once    sync.Once
}

func (collector *eventDrivenShadowCollector) Run(ctx context.Context) error {
	collector.once.Do(func() { close(collector.running) })
	<-ctx.Done()
	return nil
}

func (*eventDrivenShadowCollector) Views() marketdata.MarketViewProvider {
	return marketdata.NewProvider()
}

func (*eventDrivenShadowCollector) HealthSnapshot() exchangecontracts.CollectorHealthSnapshot {
	return exchangecontracts.CollectorHealthSnapshot{}
}

func (collector *eventDrivenShadowCollector) MarketUpdates() <-chan struct{} {
	return collector.updates
}
