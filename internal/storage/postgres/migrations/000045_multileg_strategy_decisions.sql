SET TIME ZONE 'UTC';

-- Automatic Triangular and Cross-Exchange sessions use the same immutable
-- accepted-decision records as the one-leg strategies.  The original tables
-- were introduced while only Trend and Mean Reversion were installed, so
-- their strategy checks must be expanded before a multi-leg plan can commit.
ALTER TABLE v1c_strategy_plan_decisions
  DROP CONSTRAINT v1c_strategy_plan_decisions_strategy_check,
  ADD CONSTRAINT v1c_strategy_plan_decisions_strategy_check
  CHECK (strategy IN (
    'trend','mean-reversion','triangular','cross-exchange-arbitrage'
  ));

ALTER TABLE sandbox_strategy_decisions
  DROP CONSTRAINT sandbox_strategy_decisions_strategy_check,
  ADD CONSTRAINT sandbox_strategy_decisions_strategy_check
  CHECK (strategy IN (
    'trend','mean-reversion','triangular','cross-exchange-arbitrage'
  ));
