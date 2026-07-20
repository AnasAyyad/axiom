package backtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"axiom/internal/replay"
)

// EventResult contains canonical same-pipeline output for one input event.
type EventResult struct {
	Ordinal         uint64          `json:"ordinal"`
	Decision        json.RawMessage `json:"decision"`
	Orders          json.RawMessage `json:"orders"`
	ExecutionEvents json.RawMessage `json:"execution_events,omitempty"`
	Balances        json.RawMessage `json:"balances"`
}

// Processor is the shared strategy/allocation/risk/execution/accounting path.
type Processor interface {
	Process(context.Context, replay.Event) (EventResult, error)
	Metrics() Metrics
}

// Metrics is the generic Section 21 canonical metric schema. Values are exact
// decimal strings or the literal "unavailable" when not meaningful.
type Metrics struct {
	TotalNetReturn             string            `json:"total_net_return"`
	AnnualizedReturn           string            `json:"annualized_return"`
	MaximumDrawdown            string            `json:"maximum_drawdown"`
	CurrentDrawdown            string            `json:"current_drawdown"`
	SharpeRatio                string            `json:"sharpe_ratio"`
	SortinoRatio               string            `json:"sortino_ratio"`
	CalmarRatio                string            `json:"calmar_ratio"`
	ProfitFactor               string            `json:"profit_factor"`
	Expectancy                 string            `json:"expectancy"`
	WinRate                    string            `json:"win_rate"`
	AverageWin                 string            `json:"average_win"`
	AverageLoss                string            `json:"average_loss"`
	LargestWin                 string            `json:"largest_win"`
	LargestLoss                string            `json:"largest_loss"`
	Turnover                   string            `json:"turnover"`
	Exposure                   string            `json:"exposure"`
	Trades                     uint64            `json:"trades"`
	FeesPaid                   string            `json:"fees_paid"`
	FeePercentGrossProfit      string            `json:"fee_percent_gross_profit"`
	SlippagePercentGrossProfit string            `json:"slippage_percent_gross_profit"`
	RecoveryLoss               string            `json:"recovery_loss"`
	TimeInMarket               string            `json:"time_in_market"`
	ByAsset                    map[string]string `json:"by_asset"`
	ByExchange                 map[string]string `json:"by_exchange"`
	ByStrategy                 map[string]string `json:"by_strategy"`
	ByRegime                   map[string]string `json:"by_regime"`
}

// CanonicalResult is one immutable result whose hash covers all run outputs.
type CanonicalResult struct {
	ManifestHash string         `json:"manifest_hash"`
	Confidence   ConfidenceTier `json:"confidence"`
	Namespace    ModelNamespace `json:"namespace"`
	Events       []EventResult  `json:"events"`
	Metrics      Metrics        `json:"metrics"`
	ResultHash   string         `json:"result_hash"`
}

// Engine runs every supported mode through an identical Processor contract.
type Engine struct {
	controller   *replay.Controller
	processor    Processor
	manifest     RunManifest
	manifestHash string
}

// NewEngine binds a replay controller and same-pipeline processor to one run.
func NewEngine(controller *replay.Controller, processor Processor, manifest RunManifest) (*Engine, error) {
	if controller == nil || processor == nil {
		return nil, backtestError("engine_configuration_invalid")
	}
	hash, err := manifest.CanonicalHash()
	if err != nil {
		return nil, err
	}
	return &Engine{controller: controller, processor: processor, manifest: manifest, manifestHash: hash}, nil
}

// Run resumes the controller and returns byte-stable canonical output.
func (engine *Engine) Run(ctx context.Context) (CanonicalResult, error) {
	result, _, err := engine.RunControlled(ctx, nil)
	return result, err
}

// RunControlled executes deterministically and may return an unsealed partial
// result after a durable replay control requests a safe event boundary.
func (engine *Engine) RunControlled(ctx context.Context,
	pauseAfter func(uint64) (bool, error)) (CanonicalResult, bool, error) {
	engine.controller.Resume()
	results := make([]EventResult, 0)
	for {
		event, ok, err := engine.controller.Next(ctx)
		if err != nil {
			return CanonicalResult{}, false, err
		}
		if !ok {
			break
		}
		result, err := engine.processor.Process(ctx, event)
		if err != nil || result.Ordinal != event.Ordinal {
			return CanonicalResult{}, false, backtestError("processor_output_invalid")
		}
		results = append(results, cloneEventResult(result))
		if pauseAfter != nil {
			paused, controlErr := pauseAfter(event.Ordinal)
			if controlErr != nil {
				return CanonicalResult{}, false, controlErr
			}
			if paused {
				return engine.canonicalResult(results, false)
			}
		}
	}
	return engine.canonicalResult(results, true)
}

func (engine *Engine) canonicalResult(results []EventResult, seal bool) (CanonicalResult, bool, error) {
	output := CanonicalResult{ManifestHash: engine.manifestHash, Confidence: engine.manifest.Dataset.Confidence,
		Namespace: engine.manifest.Models, Events: results, Metrics: engine.processor.Metrics()}
	if !seal {
		return output, true, nil
	}
	hash, err := resultHash(output)
	if err != nil {
		return CanonicalResult{}, false, err
	}
	output.ResultHash = hash
	return output, false, nil
}

// CompareResults rejects model-world mismatches before comparing result hashes.
func CompareResults(left, right CanonicalResult) (bool, error) {
	if !left.Namespace.Comparable(right.Namespace) {
		return false, backtestError("model_namespace_incompatible")
	}
	return left.ResultHash != "" && left.ResultHash == right.ResultHash, nil
}

func resultHash(result CanonicalResult) (string, error) {
	result.ResultHash = ""
	encoded, err := json.Marshal(result)
	if err != nil {
		return "", backtestError("result_encode_failed")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func cloneEventResult(result EventResult) EventResult {
	result.Decision = append(json.RawMessage(nil), result.Decision...)
	result.Orders = append(json.RawMessage(nil), result.Orders...)
	result.ExecutionEvents = append(json.RawMessage(nil), result.ExecutionEvents...)
	result.Balances = append(json.RawMessage(nil), result.Balances...)
	return result
}
