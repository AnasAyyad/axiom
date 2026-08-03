import type { ActivityFilters as Filters } from "../../api/queries";
import styles from "../shared/D2.module.css";

interface ActivityFiltersProps {
  readonly value: Filters;
  readonly onChange: (value: Filters) => void;
  readonly onReset: () => void;
}

const fields: ReadonlyArray<{
  key: keyof Filters;
  label: string;
  type?: string;
}> = [
  { key: "from", label: "From", type: "datetime-local" },
  { key: "to", label: "To", type: "datetime-local" },
  { key: "strategy", label: "Strategy" },
  { key: "instrument", label: "Instrument" },
  { key: "exchange", label: "Exchange" },
  { key: "outcome", label: "Outcome" },
  { key: "reason", label: "Reason code" },
  { key: "correlation_id", label: "Correlation ID" },
];

export function ActivityFilters({
  value,
  onChange,
  onReset,
}: ActivityFiltersProps) {
  const update = (key: keyof Filters, next: string) =>
    onChange({ ...value, [key]: next });
  return (
    <form
      className={styles.controlCard}
      aria-label="Activity filters"
      onSubmit={(event) => event.preventDefault()}
    >
      <div className={styles.filterGrid}>
        {fields.map((field) => (
          <label className={styles.field} key={field.key}>
            {field.label}
            <input
              type={field.type ?? "search"}
              value={value[field.key]}
              onChange={(event) => update(field.key, event.target.value)}
            />
          </label>
        ))}
        <label className={styles.field}>
          Side
          <select
            value={value.side}
            onChange={(event) => update("side", event.target.value)}
          >
            <option value="">All sides</option>
            <option value="buy">Buy</option>
            <option value="sell">Sell</option>
          </select>
        </label>
        <label className={styles.field}>
          Mode
          <select
            value={value.mode}
            onChange={(event) => update("mode", event.target.value)}
          >
            <option value="">All modes</option>
            {["backtest", "replay", "paper", "shadow", "testnet", "demo"].map(
              (mode) => (
                <option key={mode}>{mode}</option>
              ),
            )}
          </select>
        </label>
      </div>
      <div className={styles.actions}>
        <button className={styles.secondary} type="button" onClick={onReset}>
          Clear filters
        </button>
      </div>
    </form>
  );
}
