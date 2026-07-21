import styles from "./UI.module.css";

interface MetricCardProps {
  readonly label: string;
  readonly value: string;
  readonly tone?: "neutral" | "good" | "warn" | "critical";
  readonly detail?: string;
}

export function MetricCard({
  label,
  value,
  tone = "neutral",
  detail,
}: MetricCardProps) {
  return (
    <article className={styles.metric} data-tone={tone}>
      <span>{label}</span>
      <strong>{value}</strong>
      {detail && <small>{detail}</small>}
    </article>
  );
}
