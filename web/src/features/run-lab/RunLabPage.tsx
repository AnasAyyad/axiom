import { Link } from "react-router-dom";

import { Page } from "../../app/OperationalShared";
import styles from "../shared/D2.module.css";

const approvedRuns = [
  {
    title: "Backtest",
    description:
      "Run a registered strategy version against an approved immutable historical dataset.",
    to: "/backtests",
    action: "Open Backtest Lab",
  },
  {
    title: "Replay",
    description:
      "Reproduce the canonical decision pipeline with deterministic inputs, controls, and incident links.",
    to: "/replays",
    action: "Open Replay Lab",
  },
  {
    title: "Shadow",
    description:
      "Observe public-live decisions through virtual execution, accounting, and centralized risk.",
    to: "/shadow",
    action: "Open Shadow Center",
  },
  {
    title: "Sandbox qualification",
    description:
      "Operate only capped Binance Spot Testnet and Bybit Demo workflows under C6 controls.",
    to: "/operations/sandbox",
    action: "Open Sandbox Operations",
  },
  {
    title: "Formal qualification or drill",
    description:
      "Start or monitor only a version-controlled approved qualification definition.",
    to: "/operations/qualifications",
    action: "Open Qualification Center",
  },
] as const;

export function RunLabPage() {
  return (
    <Page
      title="Run Lab"
      eyebrow="Approved research and operational tests"
      description="Choose a registered workflow. The browser cannot run arbitrary commands, scripts, or unit-test names."
    >
      <p className={styles.notice} role="note">
        Historical, replay, shadow, demo, and testnet outcomes measure research
        behavior or platform readiness. They do not prove profitability.
      </p>
      <div className={styles.cardGrid}>
        {approvedRuns.map((run) => (
          <article className={styles.card} key={run.title}>
            <h2>{run.title}</h2>
            <p>{run.description}</p>
            <Link className={styles.linkButton} to={run.to}>
              {run.action}
            </Link>
          </article>
        ))}
      </div>
    </Page>
  );
}
