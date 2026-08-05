import { Link } from "react-router";

import styles from "../features/shared/ConsoleSurface.module.css";

export function WhyNothingPanel({
  title = "Why is nothing happening?",
  reason,
  nextAction,
  to,
}: {
  readonly title?: string;
  readonly reason: string;
  readonly nextAction: string;
  readonly to: string;
}) {
  return (
    <aside className={styles.notice} aria-live="polite">
      <h3>{title}</h3>
      <p>{reason}</p>
      <Link className={styles.linkButton} to={to}>
        {nextAction}
      </Link>
    </aside>
  );
}
