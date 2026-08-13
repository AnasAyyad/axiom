import styles from "./ConsoleSurface.module.css";

interface EvidenceDetailsProps {
  readonly title?: string;
  readonly summary: string;
  readonly value: unknown;
}

export function EvidenceDetails({
  title = "Technical evidence",
  summary,
  value,
}: EvidenceDetailsProps) {
  return (
    <details className={styles.evidence}>
      <summary>{title}</summary>
      <p>{summary}</p>
      <pre>{JSON.stringify(value, null, 2)}</pre>
    </details>
  );
}
