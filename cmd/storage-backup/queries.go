package main

const targetCleanQuery = `SELECT
  (SELECT count(*) FROM pg_namespace
    WHERE nspname <> 'public' AND nspname <> 'information_schema' AND nspname !~ '^pg_') +
  (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
    WHERE n.nspname <> 'information_schema' AND n.nspname !~ '^pg_') +
  (SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
    WHERE n.nspname <> 'information_schema' AND n.nspname !~ '^pg_') +
  (SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace
    WHERE n.nspname <> 'information_schema' AND n.nspname !~ '^pg_' AND t.typisdefined)`

const restoredIntegrityQuery = `SELECT
  (SELECT count(*) FROM (
    SELECT jt.id, le.asset_symbol
    FROM journal_transactions jt
    LEFT JOIN ledger_entries le ON le.transaction_id=jt.id
    GROUP BY jt.id, le.asset_symbol
    HAVING count(le.transaction_id)=0 OR
      coalesce(sum(CASE le.direction WHEN 'debit' THEN le.quantity ELSE -le.quantity END),0) <> 0
  ) journal_violations) +
  (SELECT count(*) FROM virtual_balances WHERE available < 0 OR reserved < 0) +
  (SELECT count(*) FROM positions WHERE quantity < 0) +
  (SELECT count(*) FROM (
    SELECT coalesce(v.account_id,r.account_id) AS account_id,
      coalesce(v.asset_symbol,r.asset_symbol) AS asset_symbol
    FROM virtual_balances v
    FULL OUTER JOIN (
      SELECT account_id,asset_symbol,sum(quantity) AS quantity
      FROM reservations WHERE state IN ('active','quarantined')
      GROUP BY account_id,asset_symbol
    ) r USING (account_id,asset_symbol)
    WHERE coalesce(v.reserved,0) <> coalesce(r.quantity,0)
  ) reservation_violations)`
