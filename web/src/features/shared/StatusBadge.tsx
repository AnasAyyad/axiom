import styles from "./ConsoleSurface.module.css";

function tone(value: string) {
  const normalized = value.toLowerCase();
  if (
    [
      "healthy",
      "running",
      "ready",
      "enabled",
      "applied",
      "passed",
      "normal",
      "fresh",
      "live",
      "completed",
      "succeeded",
      "continue",
      "eligible",
    ].some((item) => normalized.includes(item))
  )
    return "good";
  if (
    [
      "failed",
      "critical",
      "locked",
      "rejected",
      "error",
      "stale",
      "unavailable",
    ].some((item) => normalized.includes(item))
  )
    return "critical";
  if (
    [
      "paused",
      "blocked",
      "warning",
      "pending",
      "degraded",
      "reconnecting",
      "unknown",
    ].some((item) => normalized.includes(item))
  )
    return "warning";
  return "neutral";
}

export function StatusBadge({ value }: { readonly value: string }) {
  return (
    <span className={styles.badge} data-tone={tone(value)}>
      {value.replaceAll("_", " ")}
    </span>
  );
}
