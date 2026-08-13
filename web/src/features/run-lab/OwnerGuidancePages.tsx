import { Link } from "react-router";

import { Page } from "../../app/OperationalShared";
import { HelpDetails } from "../../components/HelpDetails";
import styles from "../shared/ConsoleSurface.module.css";

export { GuidedDemonstrationsPage } from "./GuidedDemonstrationsPage";
export { GlossaryPage } from "./GlossaryPage";

export function GettingStartedPage() {
  return (
    <Page
      title="Getting Started"
      eyebrow="A safe first tour"
      description="Axiom is spot-only research and simulation software. It cannot submit real-money production orders or move assets."
    >
      <div className={styles.cardGrid}>
        <article className={styles.card}>
          <h2>1. Check system readiness</h2>
          <p>
            Review public-data freshness, risk state, and active blockers before
            creating any research or simulation work.
          </p>
          <Link className={styles.linkButton} to="/operations">
            Review health
          </Link>
        </article>
        <article className={styles.card}>
          <h2>2. Explore reviewed strategies</h2>
          <p>
            See each strategy’s inputs, evidence limitations, and the modes the
            server has approved. No database IDs need to be copied.
          </p>
          <Link className={styles.linkButton} to="/strategies">
            Explore strategies
          </Link>
        </article>
        <article className={styles.card}>
          <h2>3. Choose a run</h2>
          <p>
            Backtests, replays, and shadow sessions remain separate forms of
            evidence. Results never establish profitability.
          </p>
          <Link className={styles.linkButton} to="/run-lab">
            Choose a run
          </Link>
        </article>
      </div>
      <section
        className={styles.section}
        aria-labelledby="first-login-checklist"
      >
        <h2 id="first-login-checklist">First-login checklist</h2>
        <p>
          Complete these in order. Optional sandbox steps remain unavailable
          until a separate exchange account is correctly configured and armed.
        </p>
        <ol className={styles.facts}>
          {firstLoginChecklist.map((item) => (
            <li key={item.title}>
              <Link className={styles.linkButton} to={item.to}>
                {item.title}
              </Link>
              <p>{item.detail}</p>
            </li>
          ))}
        </ol>
      </section>
      <HelpDetails title="What this first tour proves">
        <p>
          A guided walkthrough proves that the displayed strategy, allocation,
          risk, virtual execution, and accounting components can work together
          for its synthetic inputs. It does not predict returns or prove that a
          strategy will be profitable.
        </p>
      </HelpDetails>
    </Page>
  );
}

const firstLoginChecklist = [
  {
    title: "1. Confirm both public-data collectors",
    detail:
      "Check Binance and Bybit freshness before relying on either exchange.",
    to: "/exchanges",
  },
  {
    title: "2. Run a guided proof demonstration",
    detail:
      "Use a synthetic walkthrough before interpreting any live or historical result.",
    to: "/guided-demonstrations",
  },
  {
    title: "3. Follow one decision end to end",
    detail:
      "Open its decision, planned order, virtual fill, and accounting evidence.",
    to: "/activity/decisions-orders",
  },
  {
    title: "4. Start a live shadow session",
    detail:
      "Shadow is public-data simulation only and never creates an exchange order.",
    to: "/shadow",
  },
  {
    title: "5. Understand its next evaluation",
    detail: "Read the waiting reason and cadence before expecting a decision.",
    to: "/run-lab",
  },
  {
    title: "6. Optionally configure Binance Testnet",
    detail:
      "Review the isolated account and engine state without exposing credentials.",
    to: "/operations/sandbox",
  },
  {
    title: "7. Optionally configure Bybit Demo",
    detail:
      "Review the separate Demo account and its public-data prerequisites.",
    to: "/operations/sandbox",
  },
  {
    title: "8. Run a capped sandbox connection check",
    detail:
      "This advanced manual check remains spot-only, capped, and reconciled.",
    to: "/operations/sandbox",
  },
  {
    title: "9. Start an armed sandbox strategy session",
    detail:
      "Available only after the required strategy-session workflow is installed and armed.",
    to: "/operations/sandbox",
  },
] as const;
