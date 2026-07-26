package research

import (
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/apd/v3"
)

const eulerMascheroni = 0.5772156649015329

// MultipleTestingInput registers one Benjamini-Hochberg false-discovery family.
type MultipleTestingInput struct {
	Method     string   `json:"method"`
	Alpha      string   `json:"alpha"`
	RawPValues []string `json:"raw_p_values"`
}

// MultipleTestingEvidence contains deterministic adjusted values in original
// hypothesis order.
type MultipleTestingEvidence struct {
	Method          string   `json:"method"`
	Alpha           string   `json:"alpha"`
	RawPValues      []string `json:"raw_p_values"`
	AdjustedPValues []string `json:"adjusted_p_values"`
	Rejected        []bool   `json:"rejected"`
	FamilySize      uint32   `json:"family_size"`
}

// SharpeInput fixes the moments and independent trial count used by the
// probabilistic and deflated Sharpe analysis.
type SharpeInput struct {
	ObservedSharpe    string `json:"observed_sharpe"`
	BenchmarkSharpe   string `json:"benchmark_sharpe"`
	Skewness          string `json:"skewness"`
	ExcessKurtosis    string `json:"excess_kurtosis"`
	Observations      uint64 `json:"observations"`
	IndependentTrials uint32 `json:"independent_trials"`
}

// SharpeEvidence is non-authoritative statistical research output. Its
// fixed-width decimal strings never authorize an order or ledger mutation.
type SharpeEvidence struct {
	SharpeInput
	ProbabilisticSharpeProbability string `json:"probabilistic_sharpe_probability"`
	DeflatedBenchmarkSharpe        string `json:"deflated_benchmark_sharpe"`
	DeflatedSharpeProbability      string `json:"deflated_sharpe_probability"`
	Algorithm                      string `json:"algorithm"`
}

// AdjustMultipleTests applies the preregistered Benjamini-Hochberg procedure.
func AdjustMultipleTests(input MultipleTestingInput) (MultipleTestingEvidence, error) {
	if input.Method != "benjamini_hochberg_fdr.v1" ||
		!validProbability(input.Alpha) || len(input.RawPValues) == 0 {
		return MultipleTestingEvidence{}, researchError("multiple_testing_invalid")
	}
	alpha, _ := strconv.ParseFloat(input.Alpha, 64)
	values := make([]float64, len(input.RawPValues))
	for index, value := range input.RawPValues {
		if !validProbability(value) {
			return MultipleTestingEvidence{}, researchError("multiple_testing_invalid")
		}
		values[index], _ = strconv.ParseFloat(value, 64)
	}
	adjusted := benjaminiHochberg(values)
	evidence := MultipleTestingEvidence{Method: input.Method, Alpha: probabilityString(alpha),
		RawPValues: make([]string, len(values)), AdjustedPValues: make([]string, len(values)),
		Rejected: make([]bool, len(values)), FamilySize: uint32(len(values))}
	for index := range values {
		evidence.RawPValues[index] = probabilityString(values[index])
		evidence.AdjustedPValues[index] = probabilityString(adjusted[index])
		evidence.Rejected[index] = adjusted[index] <= alpha
	}
	return evidence, nil
}

// AnalyzeSharpe computes the Bailey-Lopez de Prado probabilistic Sharpe ratio
// and its expected-maximum-trial deflation.
func AnalyzeSharpe(input SharpeInput) (SharpeEvidence, error) {
	observed, benchmark, skewness, excess, ok := parseSharpeInput(input)
	if !ok {
		return SharpeEvidence{}, researchError("sharpe_input_invalid")
	}
	variance := sharpeVariance(observed, skewness, excess, input.Observations)
	if variance <= 0 || math.IsNaN(variance) || math.IsInf(variance, 0) {
		return SharpeEvidence{}, researchError("sharpe_input_invalid")
	}
	deflatedBenchmark := benchmark
	if input.IndependentTrials > 1 {
		expectedMaximum := expectedMaximumSharpe(math.Sqrt(variance), int(input.IndependentTrials))
		deflatedBenchmark = math.Max(deflatedBenchmark, expectedMaximum)
	}
	probabilistic := standardNormalCDF((observed - benchmark) / math.Sqrt(variance))
	deflated := standardNormalCDF((observed - deflatedBenchmark) / math.Sqrt(variance))
	return SharpeEvidence{SharpeInput: normalizedSharpeInput(input),
		ProbabilisticSharpeProbability: probabilityString(probabilistic),
		DeflatedBenchmarkSharpe:        statisticString(deflatedBenchmark),
		DeflatedSharpeProbability:      probabilityString(deflated),
		Algorithm:                      "bailey_lopez_de_prado_psr_dsr.v1"}, nil
}

func benjaminiHochberg(values []float64) []float64 {
	indexes := make([]int, len(values))
	for index := range indexes {
		indexes[index] = index
	}
	sort.SliceStable(indexes, func(left, right int) bool {
		return values[indexes[left]] < values[indexes[right]]
	})
	adjusted := make([]float64, len(values))
	next := 1.0
	for rank := len(indexes); rank > 0; rank-- {
		index := indexes[rank-1]
		next = math.Min(next, values[index]*float64(len(values))/float64(rank))
		adjusted[index] = next
	}
	return adjusted
}

func parseSharpeInput(input SharpeInput) (float64, float64, float64, float64, bool) {
	if input.Observations < 3 || input.IndependentTrials == 0 {
		return 0, 0, 0, 0, false
	}
	values := []string{input.ObservedSharpe, input.BenchmarkSharpe, input.Skewness, input.ExcessKurtosis}
	parsed := make([]float64, len(values))
	for index, value := range values {
		decimal, _, err := parseFiniteDecimal(value)
		if err != nil {
			return 0, 0, 0, 0, false
		}
		parsed[index], _ = strconv.ParseFloat(decimal.Text('f'), 64)
		if math.Abs(parsed[index]) > 100 {
			return 0, 0, 0, 0, false
		}
	}
	return parsed[0], parsed[1], parsed[2], parsed[3], true
}

func sharpeVariance(observed, skewness, excess float64, observations uint64) float64 {
	numerator := 1 - skewness*observed + ((excess+2)/4)*observed*observed
	return numerator / float64(observations-1)
}

func expectedMaximumSharpe(standardError float64, trials int) float64 {
	first := inverseStandardNormal(1 - 1/float64(trials))
	second := inverseStandardNormal(1 - 1/(float64(trials)*math.E))
	return standardError * ((1-eulerMascheroni)*first + eulerMascheroni*second)
}

func standardNormalCDF(value float64) float64 {
	return 0.5 * (1 + math.Erf(value/math.Sqrt2))
}

func inverseStandardNormal(probability float64) float64 {
	const low, high = 0.02425, 1 - 0.02425
	a := [...]float64{-39.69683028665376, 220.9460984245205, -275.9285104469687,
		138.357751867269, -30.66479806614716, 2.506628277459239}
	b := [...]float64{-54.47609879822406, 161.5858368580409, -155.6989798598866,
		66.80131188771972, -13.28068155288572}
	c := [...]float64{-0.007784894002430293, -0.3223964580411365, -2.400758277161838,
		-2.549732539343734, 4.374664141464968, 2.938163982698783}
	d := [...]float64{0.007784695709041462, 0.3224671290700398, 2.445134137142996,
		3.754408661907416}
	if probability < low {
		q := math.Sqrt(-2 * math.Log(probability))
		return (((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
			((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
	}
	if probability <= high {
		q := probability - 0.5
		r := q * q
		return (((((a[0]*r+a[1])*r+a[2])*r+a[3])*r+a[4])*r + a[5]) * q /
			(((((b[0]*r+b[1])*r+b[2])*r+b[3])*r+b[4])*r + 1)
	}
	q := math.Sqrt(-2 * math.Log(1-probability))
	return -(((((c[0]*q+c[1])*q+c[2])*q+c[3])*q+c[4])*q + c[5]) /
		((((d[0]*q+d[1])*q+d[2])*q+d[3])*q + 1)
}

func normalizedSharpeInput(input SharpeInput) SharpeInput {
	input.ObservedSharpe = normalizeStatistic(input.ObservedSharpe)
	input.BenchmarkSharpe = normalizeStatistic(input.BenchmarkSharpe)
	input.Skewness = normalizeStatistic(input.Skewness)
	input.ExcessKurtosis = normalizeStatistic(input.ExcessKurtosis)
	return input
}

func validProbability(value string) bool {
	parsed, _, err := parseFiniteDecimal(value)
	zero, _, _ := apd.NewFromString("0")
	one, _, _ := apd.NewFromString("1")
	return err == nil && parsed.Cmp(zero) >= 0 && parsed.Cmp(one) <= 0
}

func parseFiniteDecimal(value string) (*apd.Decimal, apd.Condition, error) {
	parsed, condition, err := apd.NewFromString(value)
	if err != nil || parsed.Form != apd.Finite {
		return nil, condition, researchError("statistic_invalid")
	}
	return parsed, condition, nil
}

func normalizeStatistic(value string) string {
	parsed, _, err := parseFiniteDecimal(value)
	if err != nil {
		return ""
	}
	return decimalString(*parsed)
}

func probabilityString(value float64) string {
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(value, 'f', 12, 64), "0"), ".")
}

func statisticString(value float64) string {
	return strings.TrimRight(strings.TrimRight(strconv.FormatFloat(value, 'f', 12, 64), "0"), ".")
}
