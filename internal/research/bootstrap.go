package research

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strconv"

	"github.com/cockroachdb/apd/v3"
)

var statisticalContext = apd.Context{Precision: 38, MaxExponent: 96, MinExponent: -96,
	Traps: apd.DefaultTraps, Rounding: apd.RoundHalfEven}

// ConfidenceInterval is one deterministic block-bootstrap mean interval.
type ConfidenceInterval struct {
	Lower      string `json:"lower"`
	Point      string `json:"point"`
	Upper      string `json:"upper"`
	Iterations int    `json:"iterations"`
	BlockSize  int    `json:"block_size"`
	SeedHash   string `json:"seed_hash"`
}

// BlockBootstrapMean preserves serial blocks and uses a registered deterministic seed.
func BlockBootstrapMean(returns []string, blockSize, iterations int, seed string) (ConfidenceInterval, error) {
	if len(returns) < 2 || blockSize <= 0 || blockSize > len(returns) || iterations < 40 || seed == "" {
		return ConfidenceInterval{}, researchError("bootstrap_configuration_invalid")
	}
	values := make([]apd.Decimal, len(returns))
	for index, value := range returns {
		parsed, _, err := apd.NewFromString(value)
		if err != nil || parsed.Form != apd.Finite {
			return ConfidenceInterval{}, researchError("bootstrap_value_invalid")
		}
		values[index] = *parsed
	}
	point, err := decimalMean(values)
	if err != nil {
		return ConfidenceInterval{}, err
	}
	means := make([]apd.Decimal, iterations)
	for iteration := 0; iteration < iterations; iteration++ {
		sample := make([]apd.Decimal, 0, len(values))
		for block := 0; len(sample) < len(values); block++ {
			start := deterministicIndex(seed, iteration, block, len(values))
			for offset := 0; offset < blockSize && len(sample) < len(values); offset++ {
				sample = append(sample, values[(start+offset)%len(values)])
			}
		}
		mean, meanErr := decimalMean(sample)
		if meanErr != nil {
			return ConfidenceInterval{}, meanErr
		}
		means[iteration] = mean
	}
	sort.Slice(means, func(left, right int) bool { return means[left].Cmp(&means[right]) < 0 })
	lowerIndex := iterations * 25 / 1000
	upperIndex := (iterations*975 + 999) / 1000
	if upperIndex >= iterations {
		upperIndex = iterations - 1
	}
	seedDigest := sha256.Sum256([]byte(seed))
	return ConfidenceInterval{Lower: decimalString(means[lowerIndex]), Point: decimalString(point),
		Upper: decimalString(means[upperIndex]), Iterations: iterations, BlockSize: blockSize,
		SeedHash: fmtHex(seedDigest[:])}, nil
}

func decimalMean(values []apd.Decimal) (apd.Decimal, error) {
	var sum apd.Decimal
	sum.SetInt64(0)
	for index := range values {
		var next apd.Decimal
		if _, err := statisticalContext.Add(&next, &sum, &values[index]); err != nil {
			return apd.Decimal{}, researchError("bootstrap_arithmetic_failed")
		}
		sum = next
	}
	denominator, _, _ := apd.NewFromString(strconv.Itoa(len(values)))
	var quotient apd.Decimal
	if _, err := statisticalContext.Quo(&quotient, &sum, denominator); err != nil {
		return apd.Decimal{}, researchError("bootstrap_arithmetic_failed")
	}
	var rounded apd.Decimal
	if _, err := statisticalContext.Quantize(&rounded, &quotient, -18); err != nil {
		return apd.Decimal{}, researchError("bootstrap_arithmetic_failed")
	}
	return rounded, nil
}

func deterministicIndex(seed string, iteration, block, length int) int {
	input := []byte(seed + "|" + strconv.Itoa(iteration) + "|" + strconv.Itoa(block))
	digest := sha256.Sum256(input)
	return int(binary.BigEndian.Uint64(digest[:8]) % uint64(length))
}

func decimalString(value apd.Decimal) string {
	var reduced apd.Decimal
	reduced.Reduce(&value)
	return reduced.Text('f')
}
func fmtHex(value []byte) string {
	const digits = "0123456789abcdef"
	result := make([]byte, len(value)*2)
	for i, b := range value {
		result[i*2] = digits[b>>4]
		result[i*2+1] = digits[b&15]
	}
	return string(result)
}
