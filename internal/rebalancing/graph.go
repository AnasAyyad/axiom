package rebalancing

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"axiom/internal/domain"
)

const canonicalTimeFormat = "2006-01-02T15:04:05.000000000Z"

var boundedIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:/-]{0,191}$`)

// Error is one bounded fail-closed optimizer rejection.
type Error struct{ Code string }

// Error returns the stable optimizer-scoped rejection code.
func (failure *Error) Error() string { return "rebalancing:" + failure.Code }

func routeError(code string) error { return &Error{Code: code} }

// Graph is an immutable deterministic collection of versioned route facts.
type Graph struct {
	edges []Edge
}

// NewGraph validates immutable provenance and rejects duplicate or ambiguous
// logical fact versions.
func NewGraph(edges []Edge) (*Graph, error) {
	if len(edges) == 0 {
		return nil, routeError("facts_missing")
	}
	cloned := make([]Edge, len(edges))
	versions := make(map[string]struct{}, len(edges))
	identities := make(map[string]struct{}, len(edges))
	for index, edge := range edges {
		cloned[index] = cloneEdge(edge)
		if err := validateEdgeStructure(cloned[index]); err != nil {
			return nil, err
		}
		versionKey := logicalEdgeKey(edge) + "#" + strconvUint(edge.Version)
		if _, duplicate := versions[versionKey]; duplicate {
			return nil, routeError("fact_ambiguous")
		}
		versions[versionKey] = struct{}{}
		if _, duplicate := identities[edge.ID]; duplicate {
			return nil, routeError("fact_ambiguous")
		}
		identities[edge.ID] = struct{}{}
	}
	sort.Slice(cloned, func(left, right int) bool {
		leftKey, rightKey := logicalEdgeKey(cloned[left]), logicalEdgeKey(cloned[right])
		if leftKey != rightKey {
			return leftKey < rightKey
		}
		if cloned[left].Version != cloned[right].Version {
			return cloned[left].Version > cloned[right].Version
		}
		return cloned[left].ID < cloned[right].ID
	})
	return &Graph{edges: cloned}, nil
}

func validateEdgeStructure(edge Edge) error {
	if !boundedIdentifier.MatchString(edge.ID) || edge.Version == 0 ||
		!validNode(edge.From) || !validNode(edge.To) || edge.From == edge.To ||
		!boundedIdentifier.MatchString(edge.Provenance.Source) ||
		!boundedIdentifier.MatchString(edge.Provenance.Observer) ||
		edge.Provenance.ObservedAt.IsZero() || edge.Provenance.ExpiresAt.IsZero() ||
		edge.Provenance.ObservedAt.Location() != time.UTC ||
		edge.Provenance.ExpiresAt.Location() != time.UTC ||
		!edge.Provenance.ExpiresAt.After(edge.Provenance.ObservedAt) ||
		edge.Provenance.Confidence.Compare(mustPercent("1")) > 0 ||
		edge.MinimumDuration <= 0 || edge.MaximumDuration < edge.MinimumDuration ||
		edge.RiskScore.Compare(mustPercent("1")) > 0 ||
		!validHash(edge.Provenance.Hash) || edge.Provenance.Hash != edgeHash(edge) {
		return routeError("fact_invalid")
	}
	if edge.Provenance.Approval.Approved {
		approval := edge.Provenance.Approval
		if !boundedIdentifier.MatchString(approval.Actor) ||
			!boundedIdentifier.MatchString(approval.Reference) ||
			approval.ApprovedAt.IsZero() || approval.ApprovedAt.Location() != time.UTC ||
			approval.ApprovedAt.Before(edge.Provenance.ObservedAt) ||
			approval.ApprovedAt.After(edge.Provenance.ExpiresAt) {
			return routeError("fact_invalid")
		}
	} else if edge.Provenance.Approval.Actor != "" ||
		edge.Provenance.Approval.Reference != "" ||
		!edge.Provenance.Approval.ApprovedAt.IsZero() {
		return routeError("fact_invalid")
	}
	switch edge.Kind {
	case TradeEdge:
		if edge.From.Exchange != edge.To.Exchange || edge.From.Asset == edge.To.Asset ||
			!boundedIdentifier.MatchString(edge.Instrument) || edge.Network != "" ||
			edge.SourceChain != "" || edge.DestinationChain != "" {
			return routeError("trade_fact_invalid")
		}
	case TransferEdge:
		if edge.From.Exchange == edge.To.Exchange || edge.From.Asset != edge.To.Asset ||
			edge.Instrument != "" || !boundedIdentifier.MatchString(edge.Network) ||
			!boundedIdentifier.MatchString(edge.SourceChain) ||
			!boundedIdentifier.MatchString(edge.DestinationChain) {
			return routeError("transfer_fact_invalid")
		}
	default:
		return routeError("fact_kind_invalid")
	}
	if _, err := edge.Costs.Total(); err != nil {
		return err
	}
	return nil
}

func validNode(node Node) bool {
	if !boundedIdentifier.MatchString(node.Exchange) || node.Asset == "" {
		return false
	}
	parsed, err := domain.ParseAssetSymbol(string(node.Asset))
	return err == nil && parsed == node.Asset
}

func logicalEdgeKey(edge Edge) string {
	return string(edge.Kind) + "|" + nodeKey(edge.From) + "|" + nodeKey(edge.To) + "|" +
		edge.Instrument + "|" + edge.Network + "|" + edge.SourceChain + "|" + edge.DestinationChain
}

func nodeKey(node Node) string { return node.Exchange + "@" + string(node.Asset) }

func cloneEdge(edge Edge) Edge {
	cloned := edge
	cloned.Warnings = append([]string(nil), edge.Warnings...)
	cloned.ManualChecklist = append([]string(nil), edge.ManualChecklist...)
	return cloned
}

func strconvUint(value uint64) string {
	const digits = "0123456789"
	if value == 0 {
		return "0"
	}
	buffer := [20]byte{}
	index := len(buffer)
	for value > 0 {
		index--
		buffer[index] = digits[value%10]
		value /= 10
	}
	return string(buffer[index:])
}

func (graph *Graph) selectedEdges(request Request) ([]Edge, Diagnostics, error) {
	diagnostics := Diagnostics{ReviewedFacts: uint32(len(graph.edges))}
	latest := make(map[string]Edge)
	for _, edge := range graph.edges {
		key := logicalEdgeKey(edge)
		current, exists := latest[key]
		if !exists || edge.Version > current.Version {
			latest[key] = edge
		}
	}
	eligible := make([]Edge, 0, len(latest))
	for _, edge := range latest {
		if edgeEligible(edge, request) {
			eligible = append(eligible, cloneEdge(edge))
			diagnostics.EligibleFacts++
		} else {
			diagnostics.RejectedFacts++
		}
	}
	sort.Slice(eligible, func(left, right int) bool {
		return edgeSortKey(eligible[left]) < edgeSortKey(eligible[right])
	})
	if len(eligible) == 0 {
		return nil, diagnostics, routeError("route_unavailable")
	}
	return eligible, diagnostics, nil
}

func edgeEligible(edge Edge, request Request) bool {
	approval := edge.Provenance.Approval
	if !edge.Available || !edge.Compatible || edge.Ambiguous ||
		!approval.Approved || request.DecisionTime.Before(edge.Provenance.ObservedAt) ||
		!request.DecisionTime.Before(edge.Provenance.ExpiresAt) ||
		approval.ApprovedAt.After(request.DecisionTime) ||
		edge.Provenance.Confidence.Compare(request.Configuration.MinimumConfidence) < 0 ||
		edge.MinimumQuantity.Compare(request.Quantity) > 0 {
		return false
	}
	if !contains(request.Configuration.ApprovedAssets, string(edge.From.Asset)) ||
		!contains(request.Configuration.ApprovedAssets, string(edge.To.Asset)) ||
		!contains(request.Configuration.Exchanges, edge.From.Exchange) ||
		!contains(request.Configuration.Exchanges, edge.To.Exchange) {
		return false
	}
	if edge.Kind == TransferEdge &&
		(!edge.WithdrawalAvailable || !edge.DepositAvailable ||
			edge.SourceChain != edge.DestinationChain || edge.SourceChain != edge.Network) {
		return false
	}
	return true
}

func edgeSortKey(edge Edge) string {
	return logicalEdgeKey(edge) + "|" + strings.Repeat("0", 20-len(strconvUint(edge.Version))) +
		strconvUint(edge.Version) + "|" + edge.ID
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
