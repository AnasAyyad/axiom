import styles from "../features/shared/ConsoleSurface.module.css";

export function HelpDetails({
  title = "About this section",
  children,
}: {
  readonly title?: string;
  readonly children: React.ReactNode;
}) {
  return (
    <details className={styles.evidence}>
      <summary>{title}</summary>
      {children}
    </details>
  );
}
