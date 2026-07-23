package triangular

import (
	"strings"

	"axiom/internal/domain"
	"axiom/internal/portfolio"
	runtimecore "axiom/internal/runtime"
)

const claimStrategy = "triangular"

// ClaimCandidate acquires every capital, fee, recovery, and displayed-liquidity
// requirement in one portfolio serialization boundary.
func ClaimCandidate(
	set *portfolio.AtomicClaimSet,
	candidate Candidate,
	owner string,
	reservationID domain.ReservationID,
	fence runtimecore.FencingToken,
	nowOffsetNanos uint64,
) (portfolio.ClaimGroup, error) {
	if set == nil || candidate.ID == "" || owner == "" || len(candidate.Claims) == 0 ||
		nowOffsetNanos > candidate.ExpiresOffsetNanos {
		return portfolio.ClaimGroup{}, strategyError("candidate_claim_rejected")
	}
	items := make([]portfolio.ClaimItem, 0, len(candidate.Claims))
	for _, requirement := range candidate.Claims {
		kind, ok := claimKind(requirement.Kind)
		if !ok || requirement.Exchange != candidate.Exchange {
			return portfolio.ClaimGroup{}, strategyError("candidate_claim_rejected")
		}
		items = append(items, portfolio.ClaimItem{
			Key: portfolio.ClaimKey{
				Kind: kind, Owner: strings.ToLower(owner),
				Exchange: strings.ToLower(requirement.Exchange),
				Resource: strings.ToLower(requirement.Resource),
			},
			Quantity: requirement.Quantity,
		})
	}
	group, err := set.ClaimAtomically(reservationID, claimStrategy, items, fence)
	if err != nil {
		return portfolio.ClaimGroup{}, strategyError("candidate_claim_unavailable")
	}
	return group, nil
}

func claimKind(value string) (portfolio.ClaimKind, bool) {
	switch value {
	case string(portfolio.ClaimBalance):
		return portfolio.ClaimBalance, true
	case string(portfolio.ClaimFeeBuffer):
		return portfolio.ClaimFeeBuffer, true
	case string(portfolio.ClaimLiquidity):
		return portfolio.ClaimLiquidity, true
	case string(portfolio.ClaimRecovery):
		return portfolio.ClaimRecovery, true
	default:
		return "", false
	}
}
