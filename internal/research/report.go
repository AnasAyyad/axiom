package research

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/apd/v3"
)

// DisclaimerNoProductionProfitability is mandatory on every research report.
const DisclaimerNoProductionProfitability = "Backtest, replay, paper, and shadow results are research evidence only and are not evidence or a guarantee of production profitability."

// ResultSlice is one exact deterministic research result dimension.
type ResultSlice struct {
	Name        string `json:"name"`
	NetReturn   string `json:"net_return"`
	MaxDrawdown string `json:"max_drawdown"`
	Trades      uint64 `json:"trades"`
}

// CapacityPoint is one notional sensitivity result.
type CapacityPoint struct {
	Notional  string `json:"notional"`
	NetReturn string `json:"net_return"`
	FillRate  string `json:"fill_rate"`
}

// Stability summarizes the registered parameter neighborhood.
type Stability struct {
	Stable        bool   `json:"stable"`
	Variants      int    `json:"variants"`
	Nonnegative   int    `json:"nonnegative"`
	WorstReturn   string `json:"worst_return"`
	BestDrawdown  string `json:"best_drawdown"`
	WorstDrawdown string `json:"worst_drawdown"`
}

// ReportInput is the complete registered evidence needed for one report.
type ReportInput struct {
	ResearchGenerationID string                   `json:"research_generation_id"`
	Hypothesis           string                   `json:"hypothesis"`
	PrimaryMetric        string                   `json:"primary_metric"`
	Split                ChronologicalSplit       `json:"split"`
	WalkForward          []WalkForwardFold        `json:"walk_forward"`
	Confidence           ConfidenceInterval       `json:"confidence"`
	Neighborhood         []ResultSlice            `json:"neighborhood"`
	Capacity             []CapacityPoint          `json:"capacity"`
	Stress               []ResultSlice            `json:"stress"`
	Benchmarks           []ResultSlice            `json:"benchmarks"`
	Breakdowns           map[string][]ResultSlice `json:"breakdowns"`
	Rejections           map[string]uint64        `json:"rejections"`
	RunReferences        []string                 `json:"run_references"`
	ConfidenceLabel      string                   `json:"confidence_label"`
	PlatformCorrectness  string                   `json:"platform_correctness"`
	StrategyEvidence     string                   `json:"strategy_evidence"`
	ViabilityDisposition string                   `json:"viability_disposition"`
	CreatedAt            time.Time                `json:"created_at"`
}

// ReportManifest is immutable canonical A10 report evidence.
type ReportManifest struct {
	ReportInput
	Stability    Stability `json:"stability"`
	Disclaimer   string    `json:"disclaimer"`
	ManifestHash string    `json:"manifest_hash"`
}

// BuildReport validates coverage, computes stability, and hashes canonical JSON.
func BuildReport(input ReportInput) (ReportManifest, error) {
	if err := validateReportInput(input); err != nil {
		return ReportManifest{}, err
	}
	stability, err := NeighborhoodStability(input.Neighborhood)
	if err != nil {
		return ReportManifest{}, err
	}
	manifest := ReportManifest{ReportInput: cloneReportInput(input), Stability: stability,
		Disclaimer: DisclaimerNoProductionProfitability}
	canonical, err := json.Marshal(manifest)
	if err != nil {
		return ReportManifest{}, researchError("report_serialization_failed")
	}
	digest := sha256.Sum256(canonical)
	manifest.ManifestHash = hex.EncodeToString(digest[:])
	return manifest, nil
}

// NeighborhoodStability reports exact registered-neighbor dispersion.
func NeighborhoodStability(variants []ResultSlice) (Stability, error) {
	if len(variants) < 3 {
		return Stability{}, researchError("parameter_neighborhood_incomplete")
	}
	zero, _, _ := apd.NewFromString("0")
	var worstReturn, bestDrawdown, worstDrawdown *apd.Decimal
	nonnegative := 0
	for _, variant := range variants {
		value, drawdown, err := parseResult(variant)
		if err != nil {
			return Stability{}, err
		}
		if value.Cmp(zero) >= 0 {
			nonnegative++
		}
		if worstReturn == nil || value.Cmp(worstReturn) < 0 {
			copy := value
			worstReturn = &copy
		}
		if bestDrawdown == nil || drawdown.Cmp(bestDrawdown) < 0 {
			copy := drawdown
			bestDrawdown = &copy
		}
		if worstDrawdown == nil || drawdown.Cmp(worstDrawdown) > 0 {
			copy := drawdown
			worstDrawdown = &copy
		}
	}
	return Stability{Stable: nonnegative*4 >= len(variants)*3, Variants: len(variants), Nonnegative: nonnegative,
		WorstReturn: decimalString(*worstReturn), BestDrawdown: decimalString(*bestDrawdown),
		WorstDrawdown: decimalString(*worstDrawdown)}, nil
}

func validateReportInput(input ReportInput) error {
	if input.ResearchGenerationID == "" || input.Hypothesis == "" || input.PrimaryMetric == "" ||
		len(input.WalkForward) == 0 || input.Confidence.SeedHash == "" || len(input.Capacity) < 2 ||
		len(input.RunReferences) == 0 || input.CreatedAt.Location() != time.UTC ||
		containsMisleadingClaim(input.PlatformCorrectness) || containsMisleadingClaim(input.StrategyEvidence) {
		return researchError("report_input_incomplete")
	}
	if err := input.Split.Validate(); err != nil {
		return err
	}
	if input.ConfidenceLabel != "local_tier_b" && input.ConfidenceLabel != "formal_tier_a" &&
		input.ConfidenceLabel != "insufficient" && input.ConfidenceLabel != "rejected" {
		return researchError("report_confidence_invalid")
	}
	if input.ViabilityDisposition != "undetermined" && input.ViabilityDisposition != "viable_for_more_research" &&
		input.ViabilityDisposition != "rejected" {
		return researchError("report_viability_invalid")
	}
	if err := validateCapacity(input.Capacity); err != nil {
		return err
	}
	if !containsNames(input.Stress, []string{"fee", "spread", "slippage", "latency", "gap", "missed_fill"}) ||
		!containsNames(input.Benchmarks, []string{"cash", "buy_and_hold", "static_inventory"}) {
		return researchError("report_scenarios_incomplete")
	}
	for _, key := range []string{"asset", "regime", "holding_period", "false_breakout", "drawdown"} {
		if len(input.Breakdowns[key]) == 0 {
			return researchError("report_breakdown_incomplete")
		}
	}
	return nil
}

func validateCapacity(points []CapacityPoint) error {
	var prior *apd.Decimal
	for _, point := range points {
		notional, _, err := apd.NewFromString(point.Notional)
		if err != nil || notional.Sign() <= 0 {
			return researchError("capacity_curve_invalid")
		}
		if prior != nil && notional.Cmp(prior) <= 0 {
			return researchError("capacity_curve_invalid")
		}
		if _, _, err = apd.NewFromString(point.NetReturn); err != nil {
			return researchError("capacity_curve_invalid")
		}
		fill, _, err := apd.NewFromString(point.FillRate)
		one, _, _ := apd.NewFromString("1")
		if err != nil || fill.Sign() < 0 || fill.Cmp(one) > 0 {
			return researchError("capacity_curve_invalid")
		}
		copy := *notional
		prior = &copy
	}
	return nil
}

func parseResult(result ResultSlice) (apd.Decimal, apd.Decimal, error) {
	value, _, valueErr := apd.NewFromString(result.NetReturn)
	drawdown, _, drawdownErr := apd.NewFromString(result.MaxDrawdown)
	if result.Name == "" || valueErr != nil || drawdownErr != nil || drawdown.Sign() < 0 {
		return apd.Decimal{}, apd.Decimal{}, researchError("research_result_invalid")
	}
	return *value, *drawdown, nil
}

func containsNames(results []ResultSlice, required []string) bool {
	names := make(map[string]struct{}, len(results))
	for _, result := range results {
		if _, _, err := parseResult(result); err != nil {
			return false
		}
		names[result.Name] = struct{}{}
	}
	for _, name := range required {
		if _, ok := names[name]; !ok {
			return false
		}
	}
	return true
}

func containsMisleadingClaim(value string) bool {
	lower := strings.ToLower(value)
	return value == "" || strings.Contains(lower, "guaranteed profit") || strings.Contains(lower, "production profitable") ||
		strings.Contains(lower, "will profit")
}

func cloneReportInput(input ReportInput) ReportInput {
	cloned := input
	cloned.WalkForward = append([]WalkForwardFold(nil), input.WalkForward...)
	cloned.Neighborhood = append([]ResultSlice(nil), input.Neighborhood...)
	cloned.Capacity = append([]CapacityPoint(nil), input.Capacity...)
	cloned.Stress = append([]ResultSlice(nil), input.Stress...)
	cloned.Benchmarks = append([]ResultSlice(nil), input.Benchmarks...)
	cloned.RunReferences = append([]string(nil), input.RunReferences...)
	cloned.Breakdowns = make(map[string][]ResultSlice, len(input.Breakdowns))
	for key, values := range input.Breakdowns {
		cloned.Breakdowns[key] = append([]ResultSlice(nil), values...)
	}
	cloned.Rejections = make(map[string]uint64, len(input.Rejections))
	for key, value := range input.Rejections {
		cloned.Rejections[key] = value
	}
	sort.Strings(cloned.RunReferences)
	return cloned
}
