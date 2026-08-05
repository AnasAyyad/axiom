import { useState } from "react";
import { Link } from "react-router";
import { useQuery } from "@tanstack/react-query";

import {
  guidedDemonstrationQuery,
  guidedDemonstrationsQuery,
} from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
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
  const demonstrations = useQuery(guidedDemonstrationsQuery);
  const [selectedID, setSelectedID] = useState("");
  const result = useQuery(guidedDemonstrationQuery(selectedID));
  return (
    <Page
      title="Guided Demonstrations"
      eyebrow="Deterministic proof workflows"
      description="Synthetic, deterministic walkthroughs of the real shared pipeline. They are not historical performance or profitability evidence."
    >
      <section className={styles.notice} aria-live="polite">
        <h2>How demonstrations work</h2>
        <p>
          This page only lists scenarios the server can execute through the
          shared pipeline. A walkthrough never opens an account, uses a
          credential, contacts an exchange, or creates a durable run.
        </p>
      </section>
      {demonstrations.isLoading && <StatePanel state="loading" />}
      {demonstrations.isError && (
        <StatePanel
          state="error"
          detail="Guided demonstrations are temporarily unavailable."
        />
      )}
      {demonstrations.data?.items.length === 0 && (
        <StatePanel
          state="empty"
          detail="No executable guided demonstrations are installed in this build."
        />
      )}
      <div className={styles.cardGrid}>
        {demonstrations.data?.items.map((demonstration) => (
          <article className={styles.card} key={demonstration.id}>
            <div className={styles.cardHeader}>
              <h2>{demonstration.title}</h2>
              <span className={styles.badge}>Synthetic</span>
            </div>
            <p>{demonstration.description}</p>
            <p>{demonstration.strategy_version}</p>
            <ul>
              {demonstration.expected_outcomes.map((outcome) => (
                <li key={outcome}>{outcome}</li>
              ))}
            </ul>
            <button
              className={styles.button}
              onClick={() => setSelectedID(demonstration.id)}
              type="button"
            >
              Run walkthrough
            </button>
          </article>
        ))}
      </div>
      {selectedID !== "" && result.isLoading && <StatePanel state="loading" />}
      {result.isError && (
        <StatePanel
          state="error"
          detail="The selected walkthrough could not be reproduced."
        />
      )}
      {result.data && (
        <section
          className={styles.section}
          aria-labelledby="walkthrough-evidence"
        >
          <h2 id="walkthrough-evidence">Walkthrough evidence</h2>
          <dl className={styles.facts}>
            <div>
              <dt>Configuration hash</dt>
              <dd>{result.data.configuration_hash}</dd>
            </div>
            <div>
              <dt>Result hash</dt>
              <dd>{result.data.result_hash}</dd>
            </div>
          </dl>
          <DemonstrationEventEvidence
            label="Accepted shared-pipeline event"
            event={result.data.accepted}
          />
          <DemonstrationEventEvidence
            label="Market-health rejection"
            event={result.data.rejected}
          />
          <EvidenceDetails
            summary="One-event synthetic metrics. These are not profitability metrics."
            title="Metric payload"
            value={formatCanonical(result.data.metrics)}
          />
        </section>
      )}
    </Page>
  );
}

function DemonstrationEventEvidence({
  label,
  event,
}: {
  readonly label: string;
  readonly event: {
    readonly ordinal: number;
    readonly decision: string;
    readonly orders: string;
    readonly execution_events: string;
    readonly balances: string;
  };
}) {
  return (
    <details className={styles.evidence}>
      <summary>{label}</summary>
      <p>Event ordinal {event.ordinal}</p>
      <EvidenceDetails
        summary="Canonical strategy decision."
        title="Decision"
        value={formatCanonical(event.decision)}
      />
      <EvidenceDetails
        summary="Canonical planned orders."
        title="Orders"
        value={formatCanonical(event.orders)}
      />
      <EvidenceDetails
        summary="Canonical simulated execution events."
        title="Virtual fills"
        value={formatCanonical(event.execution_events)}
      />
      <EvidenceDetails
        summary="Canonical virtual portfolio projection."
        title="Portfolio"
        value={formatCanonical(event.balances)}
      />
    </details>
  );
}

function formatCanonical(value: string): unknown {
  try {
    return JSON.parse(value) as unknown;
  } catch {
    return value;
  }
}
