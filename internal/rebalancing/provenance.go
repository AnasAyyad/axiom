package rebalancing

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

// SealEdge returns a defensive copy with its immutable provenance hash.
func SealEdge(edge Edge) Edge {
	edge = cloneEdge(edge)
	edge.Provenance.Hash = edgeHash(edge)
	return edge
}

func edgeHash(edge Edge) string {
	fields := []string{
		edge.ID, strconv.FormatUint(edge.Version, 10), string(edge.Kind),
		nodeKey(edge.From), nodeKey(edge.To), edge.Instrument, edge.Network,
		edge.SourceChain, edge.DestinationChain, edge.MinimumQuantity.String(),
		strconv.FormatBool(edge.Available), strconv.FormatBool(edge.WithdrawalAvailable),
		strconv.FormatBool(edge.DepositAvailable), strconv.FormatBool(edge.Compatible),
		strconv.FormatBool(edge.Ambiguous), edge.Costs.Fee.String(), edge.Costs.Spread.String(),
		edge.Costs.Depth.String(), edge.Costs.Delay.String(), edge.Costs.NetworkFee.String(),
		edge.Costs.Compatibility.String(), edge.Costs.VolatilityRisk.String(),
		edge.Costs.OperationalRisk.String(), strconv.FormatInt(int64(edge.MinimumDuration), 10),
		strconv.FormatInt(int64(edge.MaximumDuration), 10), edge.RiskScore.String(),
		strings.Join(edge.Warnings, "\x1f"), strings.Join(edge.ManualChecklist, "\x1f"),
		edge.Provenance.Source, edge.Provenance.Observer,
		edge.Provenance.ObservedAt.Format(canonicalTimeFormat),
		edge.Provenance.ExpiresAt.Format(canonicalTimeFormat), edge.Provenance.Confidence.String(),
		strconv.FormatBool(edge.Provenance.Approval.Approved),
		edge.Provenance.Approval.Actor, edge.Provenance.Approval.Reference,
		edge.Provenance.Approval.ApprovedAt.Format(canonicalTimeFormat),
	}
	var canonical strings.Builder
	for _, field := range fields {
		canonical.WriteString(strconv.Quote(field))
		canonical.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}

func validHash(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
