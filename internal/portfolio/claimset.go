package portfolio

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"sync"

	"axiom/internal/domain"
	runtimecore "axiom/internal/runtime"
)

// ClaimKind identifies one atomically owned resource class.
type ClaimKind string

// Multi-leg strategies claim owned balances and displayed liquidity together.
const (
	ClaimBalance   ClaimKind = "balance"
	ClaimFeeBuffer ClaimKind = "fee_buffer"
	ClaimLiquidity ClaimKind = "liquidity"
	ClaimRecovery  ClaimKind = "recovery"
)

// ClaimState is the restart-safe lifecycle of one all-or-nothing claim group.
type ClaimState string

// Claim groups remain unavailable while active or quarantined.
const (
	ClaimActive      ClaimState = "active"
	ClaimConsumed    ClaimState = "consumed"
	ClaimReleased    ClaimState = "released"
	ClaimExpired     ClaimState = "expired"
	ClaimQuarantined ClaimState = "quarantined"
)

var claimDimensionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.:/-]{0,159}$`)

// ClaimKey is one bounded ownership or displayed-liquidity dimension.
type ClaimKey struct {
	Kind     ClaimKind `json:"kind"`
	Owner    string    `json:"owner"`
	Exchange string    `json:"exchange"`
	Resource string    `json:"resource"`
}

// ClaimResource is the exact availability and held projection for one key.
type ClaimResource struct {
	Key       ClaimKey       `json:"key"`
	Available domain.Balance `json:"available"`
	Held      domain.Balance `json:"held"`
	Revision  uint64         `json:"revision"`
}

// ClaimItem is one requested or remaining amount in an atomic group.
type ClaimItem struct {
	Key       ClaimKey       `json:"key"`
	Quantity  domain.Balance `json:"quantity"`
	Remaining domain.Balance `json:"remaining"`
}

// ClaimGroup is one all-or-nothing, revision-fenced multi-resource claim.
type ClaimGroup struct {
	ID       domain.ReservationID     `json:"id"`
	Strategy string                   `json:"strategy"`
	Items    []ClaimItem              `json:"items"`
	State    ClaimState               `json:"state"`
	Fence    runtimecore.FencingToken `json:"fence"`
	Revision uint64                   `json:"revision"`
}

// ClaimSetState is the canonical restart checkpoint for every resource/group.
type ClaimSetState struct {
	Resources []ClaimResource `json:"resources"`
	Groups    []ClaimGroup    `json:"groups"`
}

// AtomicClaimSet owns multi-resource claims under one serialization boundary.
type AtomicClaimSet struct {
	mutex     sync.Mutex
	resources map[string]ClaimResource
	groups    map[string]ClaimGroup
}

// NewAtomicClaimSet constructs an empty multi-leg ownership boundary.
func NewAtomicClaimSet() *AtomicClaimSet {
	return &AtomicClaimSet{
		resources: make(map[string]ClaimResource),
		groups:    make(map[string]ClaimGroup),
	}
}

// OpenResource registers exact owned capacity before any claim can reference it.
func (set *AtomicClaimSet) OpenResource(key ClaimKey, available domain.Balance) error {
	set.mutex.Lock()
	defer set.mutex.Unlock()
	identity, ok := validClaimKey(key)
	if !ok {
		return portfolioError("claim_resource_invalid")
	}
	if _, exists := set.resources[identity]; exists {
		return portfolioError("claim_resource_duplicate")
	}
	zero, _ := domain.ParseBalance("0")
	set.resources[identity] = ClaimResource{
		Key: key, Available: available, Held: zero, Revision: 1,
	}
	return nil
}

// ClaimAtomically validates every requested resource before committing any of
// them. Readers can observe either the complete group or none of it.
func (set *AtomicClaimSet) ClaimAtomically(
	id domain.ReservationID,
	strategy string,
	requests []ClaimItem,
	fence runtimecore.FencingToken,
) (ClaimGroup, error) {
	set.mutex.Lock()
	defer set.mutex.Unlock()
	if id.Value() == "" || !validClaimDimension(strategy) || fence == 0 || len(requests) == 0 {
		return ClaimGroup{}, portfolioError("claim_group_invalid")
	}
	if _, exists := set.groups[id.String()]; exists {
		return ClaimGroup{}, portfolioError("claim_group_duplicate")
	}
	items := cloneClaimItems(requests)
	sortClaimItems(items)
	next, err := set.planAtomicClaim(items)
	if err != nil {
		return ClaimGroup{}, err
	}
	for identity, resource := range next {
		set.resources[identity] = resource
	}
	group := ClaimGroup{
		ID: id, Strategy: strategy, Items: items, State: ClaimActive, Fence: fence, Revision: 1,
	}
	set.groups[id.String()] = group
	return cloneClaimGroup(group), nil
}

func (set *AtomicClaimSet) planAtomicClaim(items []ClaimItem) (map[string]ClaimResource, error) {
	zero, _ := domain.ParseBalance("0")
	seen := make(map[string]struct{}, len(items))
	next := make(map[string]ClaimResource, len(items))
	for index := range items {
		identity, ok := validClaimKey(items[index].Key)
		if !ok || items[index].Quantity.Compare(zero) <= 0 {
			return nil, portfolioError("claim_group_invalid")
		}
		if _, duplicate := seen[identity]; duplicate {
			return nil, portfolioError("claim_group_duplicate_resource")
		}
		seen[identity] = struct{}{}
		resource, exists := set.resources[identity]
		if !exists {
			return nil, portfolioError("claim_resource_missing")
		}
		available, err := resource.Available.Subtract(items[index].Quantity)
		if err != nil {
			return nil, portfolioError("claim_resource_insufficient")
		}
		held, err := resource.Held.Add(items[index].Quantity)
		if err != nil {
			return nil, portfolioError("claim_resource_overflow")
		}
		resource.Available, resource.Held, resource.Revision = available, held, resource.Revision+1
		next[identity] = resource
		items[index].Remaining = items[index].Quantity
	}
	return next, nil
}

// Settle consumes exact resource amounts and, on final settlement, releases
// every unused remainder in the same atomic revision.
func (set *AtomicClaimSet) Settle(
	id domain.ReservationID,
	expectedRevision uint64,
	fence runtimecore.FencingToken,
	consumed []ClaimItem,
	final bool,
) (ClaimGroup, error) {
	set.mutex.Lock()
	defer set.mutex.Unlock()
	group, exists := set.groups[id.String()]
	if !exists || group.State != ClaimActive || group.Revision != expectedRevision || group.Fence != fence ||
		len(consumed) == 0 {
		return ClaimGroup{}, portfolioError("claim_settlement_rejected")
	}
	consumed = cloneClaimItems(consumed)
	sortClaimItems(consumed)
	if err := set.applyConsumption(&group, consumed); err != nil {
		return ClaimGroup{}, err
	}
	if final {
		if err := set.releaseRemainders(&group); err != nil {
			return ClaimGroup{}, err
		}
		group.State = ClaimConsumed
	}
	group.Revision++
	set.groups[id.String()] = group
	return cloneClaimGroup(group), nil
}

// Close releases, expires, or quarantines a complete active group atomically.
func (set *AtomicClaimSet) Close(
	id domain.ReservationID,
	expectedRevision uint64,
	fence runtimecore.FencingToken,
	next ClaimState,
) error {
	set.mutex.Lock()
	defer set.mutex.Unlock()
	group, exists := set.groups[id.String()]
	if !exists || group.State != ClaimActive || group.Revision != expectedRevision || group.Fence != fence ||
		(next != ClaimReleased && next != ClaimExpired && next != ClaimQuarantined) {
		return portfolioError("claim_transition_rejected")
	}
	if next == ClaimQuarantined {
		group.State, group.Revision = next, group.Revision+1
		set.groups[id.String()] = group
		return nil
	}
	if err := set.releaseRemainders(&group); err != nil {
		return err
	}
	group.State, group.Revision = next, group.Revision+1
	set.groups[id.String()] = group
	return nil
}

// Resource returns one consistent resource projection.
func (set *AtomicClaimSet) Resource(key ClaimKey) (ClaimResource, bool) {
	set.mutex.Lock()
	defer set.mutex.Unlock()
	identity, ok := validClaimKey(key)
	if !ok {
		return ClaimResource{}, false
	}
	resource, exists := set.resources[identity]
	return resource, exists
}

// Group returns one defensive claim-group snapshot.
func (set *AtomicClaimSet) Group(id domain.ReservationID) (ClaimGroup, bool) {
	set.mutex.Lock()
	defer set.mutex.Unlock()
	group, exists := set.groups[id.String()]
	return cloneClaimGroup(group), exists
}

// State returns a canonical checkpoint independent of map insertion order.
func (set *AtomicClaimSet) State() ClaimSetState {
	set.mutex.Lock()
	defer set.mutex.Unlock()
	state := ClaimSetState{
		Resources: make([]ClaimResource, 0, len(set.resources)),
		Groups:    make([]ClaimGroup, 0, len(set.groups)),
	}
	for _, resource := range set.resources {
		state.Resources = append(state.Resources, resource)
	}
	for _, group := range set.groups {
		state.Groups = append(state.Groups, cloneClaimGroup(group))
	}
	sort.Slice(state.Resources, func(left, right int) bool {
		leftID, _ := validClaimKey(state.Resources[left].Key)
		rightID, _ := validClaimKey(state.Resources[right].Key)
		return leftID < rightID
	})
	sort.Slice(state.Groups, func(left, right int) bool {
		return state.Groups[left].ID.String() < state.Groups[right].ID.String()
	})
	return state
}

// CanonicalClaimSetHash returns the restart/tamper comparison identity.
func CanonicalClaimSetHash(state ClaimSetState) string {
	encoded, _ := json.Marshal(state)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (set *AtomicClaimSet) applyConsumption(group *ClaimGroup, consumed []ClaimItem) error {
	zero, _ := domain.ParseBalance("0")
	if len(consumed) > len(group.Items) {
		return portfolioError("claim_settlement_rejected")
	}
	consumedByKey := make(map[string]domain.Balance, len(consumed))
	for _, item := range consumed {
		identity, ok := validClaimKey(item.Key)
		if !ok || item.Quantity.Compare(zero) <= 0 {
			return portfolioError("claim_settlement_rejected")
		}
		if _, duplicate := consumedByKey[identity]; duplicate {
			return portfolioError("claim_settlement_rejected")
		}
		consumedByKey[identity] = item.Quantity
	}
	for index := range group.Items {
		identity, _ := validClaimKey(group.Items[index].Key)
		quantity, found := consumedByKey[identity]
		if !found {
			continue
		}
		remaining, err := group.Items[index].Remaining.Subtract(quantity)
		if err != nil {
			return portfolioError("claim_settlement_rejected")
		}
		resource := set.resources[identity]
		held, err := resource.Held.Subtract(quantity)
		if err != nil {
			return portfolioError("claim_projection_invalid")
		}
		group.Items[index].Remaining = remaining
		resource.Held, resource.Revision = held, resource.Revision+1
		set.resources[identity] = resource
		delete(consumedByKey, identity)
	}
	if len(consumedByKey) != 0 {
		return portfolioError("claim_settlement_rejected")
	}
	return nil
}

func (set *AtomicClaimSet) releaseRemainders(group *ClaimGroup) error {
	for index := range group.Items {
		identity, _ := validClaimKey(group.Items[index].Key)
		resource := set.resources[identity]
		held, err := resource.Held.Subtract(group.Items[index].Remaining)
		if err != nil {
			return portfolioError("claim_projection_invalid")
		}
		available, err := resource.Available.Add(group.Items[index].Remaining)
		if err != nil {
			return portfolioError("claim_resource_overflow")
		}
		resource.Available, resource.Held, resource.Revision = available, held, resource.Revision+1
		set.resources[identity] = resource
		group.Items[index].Remaining, _ = domain.ParseBalance("0")
	}
	return nil
}

func validClaimKey(key ClaimKey) (string, bool) {
	switch key.Kind {
	case ClaimBalance, ClaimFeeBuffer, ClaimLiquidity, ClaimRecovery:
	default:
		return "", false
	}
	if !validClaimDimension(key.Owner) || !validClaimDimension(key.Exchange) ||
		!validClaimDimension(key.Resource) {
		return "", false
	}
	return string(key.Kind) + "/" + key.Owner + "/" + key.Exchange + "/" + key.Resource, true
}

func validClaimDimension(value string) bool {
	return claimDimensionPattern.MatchString(value)
}

func sortClaimItems(items []ClaimItem) {
	sort.Slice(items, func(left, right int) bool {
		leftID, _ := validClaimKey(items[left].Key)
		rightID, _ := validClaimKey(items[right].Key)
		return leftID < rightID
	})
}

func cloneClaimItems(items []ClaimItem) []ClaimItem {
	return append([]ClaimItem(nil), items...)
}

func cloneClaimGroup(group ClaimGroup) ClaimGroup {
	group.Items = cloneClaimItems(group.Items)
	return group
}
