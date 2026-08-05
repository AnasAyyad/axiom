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
import { HelpDetails } from "../../components/HelpDetails";

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

const glossary = [
  [
    "Shadow trading",
    "A public-data-only simulation that never sends an exchange order.",
  ],
  ["Backtest", "A deterministic run against approved historical inputs."],
  [
    "Replay",
    "A deterministic re-run of recorded market input in its original order.",
  ],
  [
    "Testnet",
    "Binance's isolated spot test environment; it is not real-money trading.",
  ],
  [
    "Demo",
    "Bybit's isolated Demo account boundary; it is not real-money trading.",
  ],
  [
    "Arm",
    "A short, owner-authorized period that permits capped sandbox entries after checks pass.",
  ],
  [
    "Reconciliation",
    "Comparing the authoritative account state with Axiom's durable records.",
  ],
  [
    "Stale data",
    "Required public input is older than its safety limit, so new decisions are paused.",
  ],
  [
    "Drawdown",
    "The decline from a prior portfolio high; it is a risk measure, not a prediction.",
  ],
  [
    "Realized P&L",
    "Gain or loss recorded after a position is reduced or closed.",
  ],
  [
    "Unrealized P&L",
    "The current mark-to-market change of an open virtual or sandbox position.",
  ],
  [
    "Slippage",
    "The difference between a planned price and simulated or acknowledged execution.",
  ],
  [
    "Reservation",
    "A temporary ownership claim that prevents the same balance or liquidity being used twice.",
  ],
  [
    "Risk lock",
    "A fail-closed risk state that blocks new entries until its recovery conditions are met.",
  ],
  [
    "Confidence tier",
    "How strong and complete the supporting input and evidence are; it is not profitability.",
  ],
  [
    "Qualification",
    "A separately governed verification process. Passing a product screen does not certify profitability.",
  ],
] as const;

export function GlossaryPage() {
  return (
    <Page
      title="Glossary"
      eyebrow="Plain-English product terms"
      description="Definitions used throughout the owner console. Technical identifiers remain in advanced evidence details."
    >
      <section className={styles.notice}>
        <h2>How to use these terms</h2>
        <p>
          These definitions explain what a value or workflow means and what it
          does not prove. They do not relax any safety limit.
        </p>
      </section>
      <dl className={styles.facts}>
        {glossary.map(([term, definition]) => (
          <div key={term}>
            <dt>{term}</dt>
            <dd>{definition}</dd>
          </div>
        ))}
      </dl>
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
