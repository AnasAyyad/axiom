package execution

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"axiom/internal/domain"
)

// ClientOrderIdentity contains non-secret trace dimensions for one attempt.
type ClientOrderIdentity struct {
	Mode       string
	StrategyID domain.StrategyID
	DecisionID domain.DecisionID
	Leg        uint32
	Attempt    uint32
}

// GenerateClientOrderID returns a deterministic collision-resistant bounded ID.
func GenerateClientOrderID(identity ClientOrderIdentity) (string, error) {
	if identity.Mode == "" || identity.StrategyID.Value() == "" || identity.DecisionID.Value() == "" ||
		identity.Attempt == 0 {
		return "", executionError("client_order_identity_invalid")
	}
	input := fmt.Sprintf("%s|%s|%s|%d|%d", identity.Mode, identity.StrategyID.String(),
		identity.DecisionID.String(), identity.Leg, identity.Attempt)
	digest := sha256.Sum256([]byte(input))
	return "ax-" + hex.EncodeToString(digest[:16]), nil
}
