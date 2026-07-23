package triangular

import (
	"time"

	"axiom/internal/risk"
)

func newB4PerformanceRiskEngine(now time.Time) (*risk.Engine, error) {
	engine, err := risk.NewEngine(&triangularRiskAudit{}, &triangularRiskAlerts{})
	if err != nil {
		return nil, err
	}
	if err = engine.ManualTransition(risk.StateNormal, triangularRecoveryEvidence(now)); err != nil {
		return nil, err
	}
	return engine, nil
}
