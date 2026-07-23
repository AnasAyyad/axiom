package rebalancing

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"
	"strings"
	"time"

	"axiom/internal/domain"
)

type routeCandidate struct {
	method          RecommendationMethod
	steps           []Step
	costs           CostBreakdown
	total           domain.Money
	minimumDuration time.Duration
	maximumDuration time.Duration
	risk            domain.Percent
	key             string
}

// Optimize returns the deterministic lowest-cost eligible advisory route,
// preferring an eligible natural reverse plan before any transfer route.
func (graph *Graph) Optimize(request Request) (Recommendation, Diagnostics, error) {
	if graph == nil || !validRequest(request) {
		return Recommendation{}, Diagnostics{}, routeError("request_invalid")
	}
	edges, diagnostics, err := graph.selectedEdges(request)
	if err != nil {
		return Recommendation{}, diagnostics, err
	}
	byID := make(map[string]Edge, len(edges))
	for _, edge := range edges {
		byID[edge.ID] = edge
	}
	natural := naturalCandidates(request, byID)
	if len(natural) > 0 {
		sortCandidates(natural)
		diagnostics.CandidatePaths = uint32(len(natural))
		return recommendationFromCandidate(request, natural[0]), diagnostics, nil
	}
	routes, routeErr := graphCandidates(request, edges)
	if routeErr != nil {
		return Recommendation{}, diagnostics, routeErr
	}
	diagnostics.CandidatePaths = uint32(len(routes))
	sortCandidates(routes)
	return recommendationFromCandidate(request, routes[0]), diagnostics, nil
}

func validRequest(request Request) bool {
	return boundedIdentifier.MatchString(request.ID) && validNode(request.Source) &&
		validNode(request.Destination) && request.Source.Exchange != request.Destination.Exchange &&
		request.Source.Asset == request.Destination.Asset && request.Quantity.Compare(balanceZero()) > 0 &&
		!request.DecisionTime.IsZero() && request.DecisionTime.Location() == time.UTC &&
		validConfiguration(request.Configuration) && validHash(request.ConfigurationHash) &&
		validHash(request.FactSetHash)
}

func naturalCandidates(request Request, facts map[string]Edge) []routeCandidate {
	result := make([]routeCandidate, 0, len(request.NaturalReversals))
	for _, plan := range request.NaturalReversals {
		sell, sellOK := facts[plan.SellFactID]
		buy, buyOK := facts[plan.BuyFactID]
		if !sellOK || !buyOK || !validNaturalPlan(request, plan, sell, buy) {
			continue
		}
		candidate, err := summarizeCandidate(NaturalReverseMethod, []Step{
			{Index: 0, Role: "sell_overweight_inventory", Fact: sell},
			{Index: 1, Role: "buy_depleted_inventory", Fact: buy},
		}, plan.ID)
		if err == nil && candidateWithinLimits(candidate, request.Configuration) {
			result = append(result, candidate)
		}
	}
	return result
}

func validNaturalPlan(request Request, plan NaturalReversalPlan, sell, buy Edge) bool {
	usdt, _ := domain.ParseAssetSymbol("USDT")
	return boundedIdentifier.MatchString(plan.ID) && boundedIdentifier.MatchString(plan.B5DecisionID) &&
		plan.Source == request.Source && plan.Destination == request.Destination &&
		sell.Kind == TradeEdge && buy.Kind == TradeEdge &&
		sell.From == request.Source &&
		sell.To == (Node{Exchange: request.Source.Exchange, Asset: usdt}) &&
		buy.From == (Node{Exchange: request.Destination.Exchange, Asset: usdt}) &&
		buy.To == request.Destination
}

func graphCandidates(request Request, edges []Edge) ([]routeCandidate, error) {
	adjacency := make(map[string][]Edge)
	for _, edge := range edges {
		adjacency[nodeKey(edge.From)] = append(adjacency[nodeKey(edge.From)], edge)
	}
	for key := range adjacency {
		sort.Slice(adjacency[key], func(left, right int) bool {
			return edgeSortKey(adjacency[key][left]) < edgeSortKey(adjacency[key][right])
		})
	}
	var result []routeCandidate
	visited := map[string]bool{nodeKey(request.Source): true}
	var walk func(Node, []Step) error
	walk = func(current Node, steps []Step) error {
		if uint32(len(steps)) >= request.Configuration.MaximumHops {
			return nil
		}
		for _, edge := range adjacency[nodeKey(current)] {
			nextKey := nodeKey(edge.To)
			if visited[nextKey] {
				continue
			}
			nextSteps := append(append([]Step(nil), steps...), Step{
				Index: uint32(len(steps)), Role: "route", Fact: cloneEdge(edge),
			})
			if edge.To == request.Destination {
				if uint32(len(result)) >= request.Configuration.MaximumCandidates {
					return routeError("search_limit_exceeded")
				}
				candidate, err := summarizeCandidate(GraphRouteMethod, nextSteps, pathKey(nextSteps))
				if err != nil {
					return err
				}
				if candidateWithinLimits(candidate, request.Configuration) {
					result = append(result, candidate)
				}
				continue
			}
			visited[nextKey] = true
			if err := walk(edge.To, nextSteps); err != nil {
				return err
			}
			delete(visited, nextKey)
		}
		return nil
	}
	if err := walk(request.Source, nil); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, routeError("route_unavailable")
	}
	return result, nil
}

func summarizeCandidate(
	method RecommendationMethod,
	steps []Step,
	key string,
) (routeCandidate, error) {
	if len(steps) == 0 {
		return routeCandidate{}, routeError("route_unavailable")
	}
	candidate := routeCandidate{method: method, steps: steps, key: key}
	for _, step := range steps {
		var err error
		candidate.costs, err = addCosts(candidate.costs, step.Fact.Costs)
		if err != nil {
			return routeCandidate{}, err
		}
		candidate.minimumDuration += step.Fact.MinimumDuration
		candidate.maximumDuration += step.Fact.MaximumDuration
		candidate.risk, err = candidate.risk.Add(step.Fact.RiskScore)
		if err != nil {
			return routeCandidate{}, routeError("risk_overflow")
		}
	}
	total, err := candidate.costs.Total()
	if err != nil {
		return routeCandidate{}, err
	}
	candidate.total = total
	return candidate, nil
}

func candidateWithinLimits(candidate routeCandidate, configuration Configuration) bool {
	return candidate.total.Compare(configuration.MaximumTotalCost) <= 0 &&
		candidate.maximumDuration <= configuration.MaximumDuration &&
		candidate.risk.Compare(configuration.MaximumRiskScore) <= 0
}

func sortCandidates(candidates []routeCandidate) {
	sort.Slice(candidates, func(left, right int) bool {
		if compared := candidates[left].total.Compare(candidates[right].total); compared != 0 {
			return compared < 0
		}
		if candidates[left].maximumDuration != candidates[right].maximumDuration {
			return candidates[left].maximumDuration < candidates[right].maximumDuration
		}
		if compared := candidates[left].risk.Compare(candidates[right].risk); compared != 0 {
			return compared < 0
		}
		if len(candidates[left].steps) != len(candidates[right].steps) {
			return len(candidates[left].steps) < len(candidates[right].steps)
		}
		return candidates[left].key < candidates[right].key
	})
}

func recommendationFromCandidate(request Request, candidate routeCandidate) Recommendation {
	warnings := []string{}
	checklist := []string{}
	for _, step := range candidate.steps {
		warnings = appendUnique(warnings, step.Fact.Warnings...)
		checklist = appendUnique(checklist, step.Fact.ManualChecklist...)
		if step.Fact.Kind == TransferEdge {
			warnings = appendUnique(warnings,
				"manual_external_action_required",
				"confirm_exact_network_chain_compatibility",
			)
		}
	}
	if candidate.method == NaturalReverseMethod {
		warnings = appendUnique(warnings, "natural_reverse_arbitrage_preferred")
	}
	checklist = appendUnique(checklist,
		"verify_facts_are_current_and_approved",
		"confirm_inventory_and_reservations_have_not_changed",
		"review_all_cost_duration_and_risk_components",
		"record_operator_decision_and_reconcile_after_manual_action",
	)
	sort.Strings(warnings)
	for len(checklist) < int(request.Configuration.MinimumChecklistSteps) {
		checklist = append(checklist, "manual_review_required_"+strconv.Itoa(len(checklist)+1))
	}
	recommendation := Recommendation{
		RequestID: request.ID, Method: candidate.method,
		Source: request.Source, Destination: request.Destination, Quantity: request.Quantity,
		Steps: cloneSteps(candidate.steps), Costs: candidate.costs, TotalCost: candidate.total,
		MinimumDuration: candidate.minimumDuration, MaximumDuration: candidate.maximumDuration,
		RiskScore: candidate.risk, Warnings: warnings, ManualChecklist: checklist,
		ConfigurationHash: request.ConfigurationHash, FactSetHash: request.FactSetHash,
		RecordedAt: request.DecisionTime, AdvisoryOnly: true,
	}
	recommendation.CanonicalHash = recommendationHash(recommendation)
	recommendation.ID = "b6-" + recommendation.CanonicalHash[:24]
	return recommendation
}

func recommendationHash(recommendation Recommendation) string {
	fields := []string{
		recommendation.RequestID, string(recommendation.Method), nodeKey(recommendation.Source),
		nodeKey(recommendation.Destination), recommendation.Quantity.String(),
		recommendation.Costs.Fee.String(), recommendation.Costs.Spread.String(),
		recommendation.Costs.Depth.String(), recommendation.Costs.Delay.String(),
		recommendation.Costs.NetworkFee.String(), recommendation.Costs.Compatibility.String(),
		recommendation.Costs.VolatilityRisk.String(), recommendation.Costs.OperationalRisk.String(),
		recommendation.TotalCost.String(), strconv.FormatInt(int64(recommendation.MinimumDuration), 10),
		strconv.FormatInt(int64(recommendation.MaximumDuration), 10), recommendation.RiskScore.String(),
		strings.Join(recommendation.Warnings, "\x1f"),
		strings.Join(recommendation.ManualChecklist, "\x1f"),
		recommendation.ConfigurationHash, recommendation.FactSetHash,
		recommendation.RecordedAt.Format(canonicalTimeFormat), strconv.FormatBool(recommendation.AdvisoryOnly),
	}
	for _, step := range recommendation.Steps {
		fields = append(fields, strconv.FormatUint(uint64(step.Index), 10), step.Role,
			step.Fact.ID, strconv.FormatUint(step.Fact.Version, 10), step.Fact.Provenance.Hash)
	}
	var canonical strings.Builder
	for _, field := range fields {
		canonical.WriteString(strconv.Quote(field))
		canonical.WriteByte('\n')
	}
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}

func pathKey(steps []Step) string {
	parts := make([]string, len(steps))
	for index, step := range steps {
		parts[index] = edgeSortKey(step.Fact)
	}
	return strings.Join(parts, "=>")
}

func appendUnique(target []string, values ...string) []string {
	seen := make(map[string]struct{}, len(target)+len(values))
	for _, value := range target {
		seen[value] = struct{}{}
	}
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		target = append(target, value)
		seen[value] = struct{}{}
	}
	return target
}

func cloneSteps(steps []Step) []Step {
	result := make([]Step, len(steps))
	for index, step := range steps {
		result[index] = step
		result[index].Fact = cloneEdge(step.Fact)
	}
	return result
}

func balanceZero() domain.Balance {
	value, _ := domain.ParseBalance("0")
	return value
}
