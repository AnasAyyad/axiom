import type { APIModel } from "../api/client";
import styles from "./Page.module.css";

export function ReplayEvidence({
  inspection,
}: {
  readonly inspection: NonNullable<
    APIModel<"JobResource">["replay_inspection"]
  >;
}) {
  const evidence = [
    ["Canonical event", inspection.canonical_event],
    ["Canonical decision", inspection.canonical_decision],
    ["Canonical orders", inspection.canonical_orders],
    ["Canonical execution events", inspection.canonical_execution_events],
    ["Canonical balances", inspection.canonical_balances],
  ] as const;
  return (
    <div>
      <dl className={styles.facts} aria-label="Replay event identity">
        <div>
          <dt>Selected ordinal</dt>
          <dd>{inspection.ordinal}</dd>
        </div>
        <div>
          <dt>Persisted event count</dt>
          <dd>{inspection.event_count}</dd>
        </div>
        <div>
          <dt>Canonical event hash</dt>
          <dd>{inspection.event_hash}</dd>
        </div>
      </dl>
      {evidence.map(([label, value]) => (
        <details key={label}>
          <summary>{label}</summary>
          <pre className={styles.canonical}>{value}</pre>
        </details>
      ))}
    </div>
  );
}
