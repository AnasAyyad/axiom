import { Link } from "react-router";

import { Page } from "../../app/OperationalShared";
import styles from "../shared/ConsoleSurface.module.css";

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
    </Page>
  );
}

export function GuidedDemonstrationsPage() {
  return (
    <Page
      title="Guided Demonstrations"
      eyebrow="Deterministic proof workflows"
      description="Demonstrations will be synthetic, deterministic walkthroughs of the real shared pipeline—not historical performance evidence."
    >
      <section className={styles.notice} aria-live="polite">
        <h2>Demonstration bundles are not installed yet</h2>
        <p>
          This build does not claim guided proof scenarios that it cannot run. A
          bundle must include immutable input manifests, expected results,
          configuration and model identity, and source/build identity before it
          appears here.
        </p>
      </section>
    </Page>
  );
}
