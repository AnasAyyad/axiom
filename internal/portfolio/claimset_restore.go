package portfolio

import "axiom/internal/domain"

// RestoreAtomicClaimSet validates and restores one complete multi-resource
// checkpoint. Missing, duplicated, or inconsistent held amounts fail closed.
func RestoreAtomicClaimSet(state ClaimSetState) (*AtomicClaimSet, error) {
	if len(state.Resources) == 0 {
		return nil, portfolioError("claim_state_invalid")
	}
	set := NewAtomicClaimSet()
	expectedHeld, err := restoreClaimResources(set, state.Resources)
	if err != nil {
		return nil, err
	}
	if err = restoreClaimGroups(set, state.Groups, expectedHeld); err != nil {
		return nil, err
	}
	if err = validateRestoredHeld(set, expectedHeld); err != nil {
		return nil, err
	}
	return set, nil
}

func restoreClaimResources(
	set *AtomicClaimSet,
	resources []ClaimResource,
) (map[string]domain.Balance, error) {
	zero, _ := domain.ParseBalance("0")
	expectedHeld := make(map[string]domain.Balance, len(resources))
	for _, resource := range resources {
		identity, ok := validClaimKey(resource.Key)
		if !ok || resource.Revision == 0 {
			return nil, portfolioError("claim_state_invalid")
		}
		if _, duplicate := set.resources[identity]; duplicate {
			return nil, portfolioError("claim_state_invalid")
		}
		set.resources[identity] = resource
		expectedHeld[identity] = zero
	}
	return expectedHeld, nil
}

func restoreClaimGroups(
	set *AtomicClaimSet,
	groups []ClaimGroup,
	expectedHeld map[string]domain.Balance,
) error {
	for _, original := range groups {
		group := cloneClaimGroup(original)
		if group.ID.Value() == "" || !validClaimDimension(group.Strategy) || group.Fence == 0 ||
			group.Revision == 0 || len(group.Items) == 0 || !knownClaimState(group.State) {
			return portfolioError("claim_state_invalid")
		}
		if _, duplicate := set.groups[group.ID.String()]; duplicate {
			return portfolioError("claim_state_invalid")
		}
		sortClaimItems(group.Items)
		if err := restoreClaimItems(set, group, expectedHeld); err != nil {
			return err
		}
		set.groups[group.ID.String()] = group
	}
	return nil
}

func restoreClaimItems(
	set *AtomicClaimSet,
	group ClaimGroup,
	expectedHeld map[string]domain.Balance,
) error {
	zero, _ := domain.ParseBalance("0")
	for index, item := range group.Items {
		identity, ok := validClaimKey(item.Key)
		if !ok || item.Quantity.Compare(zero) <= 0 || item.Remaining.Compare(item.Quantity) > 0 ||
			(index > 0 && sameClaimKey(group.Items[index-1].Key, item.Key)) {
			return portfolioError("claim_state_invalid")
		}
		if _, exists := set.resources[identity]; !exists {
			return portfolioError("claim_state_invalid")
		}
		open := group.State == ClaimActive || group.State == ClaimQuarantined
		if (!open && item.Remaining.Compare(zero) != 0) ||
			(open && item.Remaining.Compare(zero) <= 0) {
			return portfolioError("claim_state_invalid")
		}
		if open {
			held, err := expectedHeld[identity].Add(item.Remaining)
			if err != nil {
				return portfolioError("claim_state_invalid")
			}
			expectedHeld[identity] = held
		}
	}
	return nil
}

func validateRestoredHeld(
	set *AtomicClaimSet,
	expectedHeld map[string]domain.Balance,
) error {
	for identity, resource := range set.resources {
		if resource.Held.Compare(expectedHeld[identity]) != 0 {
			return portfolioError("claim_state_invalid")
		}
	}
	return nil
}

func knownClaimState(state ClaimState) bool {
	return state == ClaimActive || state == ClaimConsumed || state == ClaimReleased ||
		state == ClaimExpired || state == ClaimQuarantined
}

func sameClaimKey(left, right ClaimKey) bool {
	leftIdentity, leftOK := validClaimKey(left)
	rightIdentity, rightOK := validClaimKey(right)
	return leftOK && rightOK && leftIdentity == rightIdentity
}
