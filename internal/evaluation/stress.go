package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"axiom/internal/domain"
	"axiom/internal/execution"
	"axiom/internal/replay"
)

// FocusedStressResult is immutable evidence from one server-owned,
// credential-free fault scenario. These checks exercise production replay and
// order-reduction boundaries; they never submit an exchange order.
type FocusedStressResult struct {
	Scenario     string            `json:"scenario"`
	Passed       bool              `json:"passed"`
	ReasonCode   string            `json:"reason_code,omitempty"`
	Evidence     map[string]string `json:"evidence"`
	EvidenceHash string            `json:"evidence_hash"`
}

var focusedStressScenarios = []string{
	"delayed_data",
	"data_gap",
	"restart_recovery",
	"rejection",
	"partial_fill",
	"cancel_fill_race",
	"unknown_result",
	"persistence_failure",
}

// RunFocusedStressSuite executes the reviewed shared-failure matrix in a
// deterministic order. A scenario failure is returned as preserved evidence,
// not an early return, so the campaign report explains every attempted check.
func RunFocusedStressSuite() []FocusedStressResult {
	results := make([]FocusedStressResult, 0, len(focusedStressScenarios))
	for _, scenario := range focusedStressScenarios {
		evidence, err := runFocusedStressScenario(scenario)
		result := FocusedStressResult{Scenario: scenario, Passed: err == nil, Evidence: evidence}
		if result.Evidence == nil {
			result.Evidence = map[string]string{}
		}
		if err != nil {
			result.ReasonCode = "STRESS_ASSERTION_FAILED"
			result.Evidence["failure"] = err.Error()
		}
		result.EvidenceHash = focusedStressResultHash(result)
		results = append(results, result)
	}
	return results
}

func focusedStressResultHash(result FocusedStressResult) string {
	result.EvidenceHash = ""
	payload, _ := json.Marshal(result)
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func runFocusedStressScenario(scenario string) (map[string]string, error) {
	switch scenario {
	case "delayed_data":
		return stressDelayedData()
	case "data_gap":
		return stressDataGap()
	case "restart_recovery":
		return stressRestartRecovery()
	case "rejection":
		return stressRejection()
	case "partial_fill":
		return stressPartialFill()
	case "cancel_fill_race":
		return stressCancelFillRace()
	case "unknown_result":
		return stressUnknownResult()
	case "persistence_failure":
		return stressPersistenceFailure()
	default:
		return nil, fmt.Errorf("evaluation_stress_scenario_unknown")
	}
}

func stressDelayedData() (map[string]string, error) {
	delay := 2 * time.Second
	observed := 0
	source, err := replay.NewFaultSource(newStressReplaySource(1),
		[]replay.Fault{{Kind: replay.FaultLatency, Ordinal: 1, Delay: delay}},
		func(event replay.FaultEvent) error {
			if event.Kind != replay.FaultLatency || event.Ordinal != 1 {
				return fmt.Errorf("evaluation_stress_latency_observation_invalid")
			}
			observed++
			return nil
		})
	if err != nil {
		return nil, err
	}
	event, ok, err := source.Next()
	if err != nil || !ok || event.Ordinal != 1 || event.LogicalTime != uint64(10+delay) || observed != 1 {
		return nil, fmt.Errorf("evaluation_stress_latency_not_applied")
	}
	return map[string]string{"outcome": "delivered_after_delay", "delay_nanos": fmt.Sprint(delay.Nanoseconds())}, nil
}

func stressDataGap() (map[string]string, error) {
	observed := 0
	source, err := replay.NewFaultSource(newStressReplaySource(2),
		[]replay.Fault{{Kind: replay.FaultSequenceGap, Ordinal: 1}},
		func(event replay.FaultEvent) error {
			if event.Kind != replay.FaultSequenceGap || event.Ordinal != 1 {
				return fmt.Errorf("evaluation_stress_gap_observation_invalid")
			}
			observed++
			return nil
		})
	if err != nil {
		return nil, err
	}
	event, ok, err := source.Next()
	if err != nil || !ok || event.Ordinal != 2 || observed != 1 {
		return nil, fmt.Errorf("evaluation_stress_gap_not_isolated")
	}
	return map[string]string{"outcome": "gap_skipped_without_fabrication", "next_ordinal": "2"}, nil
}

func stressRestartRecovery() (map[string]string, error) {
	source, err := replay.NewFaultSource(newStressReplaySource(1),
		[]replay.Fault{{Kind: replay.FaultRestart, Ordinal: 1}}, func(replay.FaultEvent) error { return nil })
	if err != nil {
		return nil, err
	}
	if _, _, err = source.Next(); err == nil || !strings.Contains(err.Error(), "fault_restart_at_event") {
		return nil, fmt.Errorf("evaluation_stress_restart_did_not_interrupt")
	}
	event, ok, err := source.Next()
	if err != nil || !ok || event.Ordinal != 1 {
		return nil, fmt.Errorf("evaluation_stress_restart_lost_boundary")
	}
	return map[string]string{"outcome": "boundary_replayed", "ordinal": "1"}, nil
}

func stressRejection() (map[string]string, error) {
	reducer, err := newStressOrderReducer()
	if err != nil {
		return nil, err
	}
	result, err := reducer.Reduce(stressOrderEvent(execution.OrderRejected, 1, "0", nil))
	if err != nil || !result.Applied || result.Order.State != execution.OrderRejected || len(result.Order.Fills) != 0 {
		return nil, fmt.Errorf("evaluation_stress_rejection_not_terminal")
	}
	return map[string]string{"outcome": "rejected_without_fill", "state": string(result.Order.State)}, nil
}

func stressPartialFill() (map[string]string, error) {
	reducer, err := acknowledgedStressOrderReducer()
	if err != nil {
		return nil, err
	}
	fill := stressFill("stress-partial", "0.4", 6)
	result, err := reducer.Reduce(stressOrderEvent(execution.OrderPartiallyFilled, 6, "0.4", []execution.FillFact{fill}))
	if err != nil || !result.Applied || result.Order.State != execution.OrderPartiallyFilled ||
		result.Order.CumulativeQuantity.String() != "0.4" || len(result.Order.Fills) != 1 {
		return nil, fmt.Errorf("evaluation_stress_partial_fill_not_exact")
	}
	return map[string]string{"outcome": "partial_fill_preserved", "cumulative_quantity": "0.4"}, nil
}

func stressCancelFillRace() (map[string]string, error) {
	reducer, err := acknowledgedStressOrderReducer()
	if err != nil {
		return nil, err
	}
	for _, event := range []execution.OrderEvent{
		stressOrderEvent(execution.OrderCancelPending, 6, "0", nil),
		stressOrderEvent(execution.OrderPartiallyFilled, 7, "0.4", []execution.FillFact{stressFill("stress-race-one", "0.4", 7)}),
		stressOrderEvent(execution.OrderCanceled, 8, "0.4", nil),
	} {
		if _, err = reducer.Reduce(event); err != nil {
			return nil, err
		}
	}
	late := stressOrderEvent(execution.OrderFilled, 9, "1", []execution.FillFact{stressFill("stress-race-two", "0.6", 9)})
	result, err := reducer.Reduce(late)
	if err != nil || !result.Applied || result.Order.State != execution.OrderFilled || len(result.Order.Fills) != 2 {
		return nil, fmt.Errorf("evaluation_stress_cancel_fill_race_unresolved")
	}
	return map[string]string{"outcome": "late_fill_reconciled", "fill_count": "2"}, nil
}

func stressUnknownResult() (map[string]string, error) {
	reducer, err := acknowledgedStressOrderReducer()
	if err != nil {
		return nil, err
	}
	for _, event := range []execution.OrderEvent{
		stressOrderEvent(execution.OrderUnknown, 6, "0", nil),
		stressOrderEvent(execution.OrderRecoveryRequired, 7, "0", nil),
		stressOrderEvent(execution.OrderRecovered, 8, "0", nil),
	} {
		if _, err = reducer.Reduce(event); err != nil {
			return nil, err
		}
	}
	snapshot := reducer.Snapshot()
	if snapshot.State != execution.OrderRecovered || snapshot.CumulativeQuantity.String() != "0" || len(snapshot.Fills) != 0 {
		return nil, fmt.Errorf("evaluation_stress_unknown_result_not_reconciled")
	}
	return map[string]string{"outcome": "recovered_from_durable_facts", "state": string(snapshot.State)}, nil
}

func stressPersistenceFailure() (map[string]string, error) {
	persistenceFailure := errors.New("evaluation_stress_persistence_unavailable")
	attempts := 0
	source, err := replay.NewFaultSource(newStressReplaySource(1),
		[]replay.Fault{{Kind: replay.FaultStorageFailure, Ordinal: 1}}, func(replay.FaultEvent) error {
			attempts++
			if attempts == 1 {
				return persistenceFailure
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	if _, _, err = source.Next(); !errors.Is(err, persistenceFailure) {
		return nil, fmt.Errorf("evaluation_stress_persistence_did_not_fail_closed")
	}
	if _, _, err = source.Next(); err == nil || !strings.Contains(err.Error(), "fault_storage_failure") {
		return nil, fmt.Errorf("evaluation_stress_storage_fault_not_replayed")
	}
	event, ok, err := source.Next()
	if err != nil || !ok || event.Ordinal != 1 || attempts != 2 {
		return nil, fmt.Errorf("evaluation_stress_persistence_lost_boundary")
	}
	return map[string]string{"outcome": "evidence_retry_then_boundary_replay", "observer_attempts": "2"}, nil
}

type stressReplaySource struct {
	events []replay.Event
	index  int
}

func newStressReplaySource(count int) *stressReplaySource {
	events := make([]replay.Event, count)
	for index := range events {
		events[index] = replay.Event{LogicalTime: uint64((index + 1) * 10), Ordinal: uint64(index + 1),
			Canonical: []byte{byte(index + 1)}}
	}
	return &stressReplaySource{events: events}
}

// Next returns the next deterministic focused-stress event.
func (source *stressReplaySource) Next() (replay.Event, bool, error) {
	if source.index >= len(source.events) {
		return replay.Event{}, false, nil
	}
	event := source.events[source.index]
	event.Canonical = append([]byte(nil), event.Canonical...)
	source.index++
	return event, true, nil
}

// SeekOrdinal positions the focused-stress source at an exact ordinal.
func (source *stressReplaySource) SeekOrdinal(ordinal uint64) error {
	for index, event := range source.events {
		if event.Ordinal == ordinal {
			source.index = index
			return nil
		}
	}
	return fmt.Errorf("evaluation_stress_ordinal_not_found")
}

func newStressOrderReducer() (*execution.OrderReducer, error) {
	orderID, err := domain.NewVirtualOrderID("evaluation-stress-order")
	if err != nil {
		return nil, err
	}
	planID, err := domain.NewExecutionPlanID("evaluation-stress-plan")
	if err != nil {
		return nil, err
	}
	instrument, err := domain.NewSpotInstrument("BTC", "USDT")
	if err != nil {
		return nil, err
	}
	quantity, err := domain.ParseQuantity("1")
	if err != nil {
		return nil, err
	}
	return execution.NewOrderReducer(execution.OrderIdentity{ID: orderID, PlanID: planID,
		ClientOrderID: "ax-evaluation-stress", Instrument: instrument, Side: domain.SideBuy, Quantity: quantity})
}

func acknowledgedStressOrderReducer() (*execution.OrderReducer, error) {
	reducer, err := newStressOrderReducer()
	if err != nil {
		return nil, err
	}
	for index, state := range []execution.OrderState{execution.OrderValidating, execution.OrderReserved,
		execution.OrderApproved, execution.OrderSubmitting, execution.OrderAcknowledged} {
		if _, err = reducer.Reduce(stressOrderEvent(state, uint64(index+1), "0", nil)); err != nil {
			return nil, err
		}
	}
	return reducer, nil
}

func stressOrderEvent(state execution.OrderState, ordinal uint64, cumulative string,
	fills []execution.FillFact) execution.OrderEvent {
	orderID, _ := domain.NewVirtualOrderID("evaluation-stress-order")
	quantity, _ := domain.ParseQuantity(cumulative)
	fees := []execution.FeeFact(nil)
	if cumulative != "0" {
		asset, _ := domain.ParseAssetSymbol("USDT")
		fee, _ := domain.ParseFee("0.01")
		fees = []execution.FeeFact{{Asset: asset, Total: fee}}
	}
	return execution.OrderEvent{ID: fmt.Sprintf("evaluation-stress-event-%d-%s", ordinal, strings.ToLower(string(state))),
		OrderID: orderID, ClientOrderID: "ax-evaluation-stress", State: state, ExchangeStatus: string(state),
		CumulativeQuantity: quantity, Fees: fees, Fills: fills,
		OccurredAt: time.Unix(1_700_000_000+int64(ordinal), 0).UTC(), Ordinal: ordinal}
}

func stressFill(id, quantityText string, ordinal uint64) execution.FillFact {
	fillID, _ := domain.NewVirtualFillID(id)
	quantity, _ := domain.ParseQuantity(quantityText)
	price, _ := domain.ParsePrice("100")
	fee, _ := domain.ParseFee("0.01")
	asset, _ := domain.ParseAssetSymbol("USDT")
	return execution.FillFact{ID: fillID, Quantity: quantity, Price: price, Fee: fee,
		FeeAsset: asset, Ordinal: ordinal}
}
