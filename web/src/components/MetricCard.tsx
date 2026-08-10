import { HelpPopover } from "./HelpPopover";
import styles from "./UI.module.css";

interface MetricCardProps {
  readonly label: string;
  readonly value: string;
  readonly tone?: "neutral" | "good" | "warn" | "critical";
  readonly detail?: string;
  readonly help?: string;
}

export function MetricCard({
  label,
  value,
  tone = "neutral",
  detail,
  help,
}: MetricCardProps) {
  const explanation =
    help ??
    `This is the most recently retrieved server-authoritative value for ${label}. It can become unavailable or stale when its source is not current, and it does not prove strategy profitability.`;
  return (
    <article className={styles.metric} data-tone={tone}>
      <div className={styles.metricHeader}>
        <span>{label}</span>
        <HelpPopover label={`About ${label}`}>
          <p>{explanation}</p>
        </HelpPopover>
      </div>
      <strong>{value}</strong>
      {detail && <small>{detail}</small>}
    </article>
  );
}
