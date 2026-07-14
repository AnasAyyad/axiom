package runtimecore

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"sync"
	"testing"
	"time"

	"axiom/internal/domain"
)

func TestConcurrentSchedulePermutationsProduceIdenticalResult(t *testing.T) {
	var expected string
	for iteration := 0; iteration < 20; iteration++ {
		clock, _ := NewDeterministicClock(1)
		scheduler, _ := NewDeterministicScheduler(clock, 128)
		order := make([]string, 0, 100)
		var orderMutex sync.Mutex
		start := make(chan struct{})
		var group sync.WaitGroup
		for index := 99; index >= 0; index-- {
			index := index
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				idValue := indexedID(index)
				key := schedulerKey(t, idValue, LogicalTime(index%7+2), zeroExchangeTime(), uint64(index%5+1), uint64(index+1))
				err := scheduler.Schedule(ScheduledWork{Key: key, Action: func() ([]ScheduledWork, error) {
					orderMutex.Lock()
					defer orderMutex.Unlock()
					order = append(order, idValue)
					return nil, nil
				}})
				if err != nil {
					t.Error(err)
				}
			}()
		}
		close(start)
		group.Wait()
		scheduler.Resume()
		if count, err := scheduler.Advance(20); err != nil || count != 100 {
			t.Fatalf("advance = %d, %v", count, err)
		}
		digest := sha256.Sum256([]byte(joinOrder(order)))
		actual := hex.EncodeToString(digest[:])
		if iteration == 0 {
			expected = actual
		} else if actual != expected {
			t.Fatalf("iteration %d hash = %s, want %s", iteration, actual, expected)
		}
	}
}

func TestEventBusDeclaredLoadRemainsWithinCapacity(t *testing.T) {
	gate := NewSafetyGate()
	bus, _ := NewEventBus(gate)
	partition := Partition{Kind: PartitionOrder, Key: "load"}
	_ = bus.Register(partition, ClassCritical, 64, false)
	bus.Seal()
	for batch := 0; batch < 1000; batch++ {
		for index := 0; index < 64; index++ {
			id := indexedID(batch*64 + index)
			if err := bus.Publish(partition, busEvent(t, id, ClassCritical, 1, "")); err != nil {
				t.Fatal(err)
			}
		}
		if depth := bus.Metrics(100)[PartitionOrder].Depth; depth != 64 {
			t.Fatalf("depth = %d", depth)
		}
		for range 64 {
			if _, ok := bus.Consume(partition); !ok {
				t.Fatal("accepted event missing")
			}
		}
	}
}

func BenchmarkDeterministicScheduler(b *testing.B) {
	for b.Loop() {
		clock, _ := NewDeterministicClock(1)
		scheduler, _ := NewDeterministicScheduler(clock, 1)
		id := indexedEventID(b, 1)
		_ = scheduler.Schedule(ScheduledWork{
			Key:    SchedulerKey{ScheduledTime: 2, StableID: id},
			Action: func() ([]ScheduledWork, error) { return nil, nil },
		})
		scheduler.Resume()
		_, _ = scheduler.Advance(2)
	}
}

func indexedID(index int) string {
	digits := "000000" + strconv.Itoa(index)
	return "event-" + digits[len(digits)-6:]
}

func joinOrder(order []string) string {
	result := ""
	for _, value := range order {
		result += value + "\n"
	}
	return result
}

func zeroExchangeTime() time.Time { return time.Unix(0, 0).UTC() }

func indexedEventID(tb testing.TB, index int) domain.EventID {
	tb.Helper()
	id, err := domain.NewEventID(indexedID(index))
	if err != nil {
		tb.Fatal(err)
	}
	return id
}
