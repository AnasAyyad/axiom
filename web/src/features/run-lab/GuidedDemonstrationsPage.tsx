import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  guidedDemonstrationQuery,
  guidedDemonstrationsQuery,
} from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { WhyNothingPanel } from "../../components/WhyNothingPanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import styles from "../shared/ConsoleSurface.module.css";

export function GuidedDemonstrationsPage() {
  const demonstrations = useQuery(guidedDemonstrationsQuery);
  const [selectedID, setSelectedID] = useState("");
  const [tourActive, setTourActive] = useState(false);
  const result = useQuery(guidedDemonstrationQuery(selectedID));
  const tourItems = demonstrations.data?.items ?? [];
  const selectedTourIndex = tourItems.findIndex(
    (demonstration) => demonstration.id === selectedID,
  );
  const tourStepActive = tourActive && selectedTourIndex >= 0;

  function startTour() {
    const first = tourItems[0];
    if (!first) return;
    setTourActive(true);
    setSelectedID(first.id);
  }

  function selectWalkthrough(id: string) {
    setTourActive(false);
    setSelectedID(id);
  }

  function continueTour() {
    const next = tourItems[selectedTourIndex + 1];
    if (!next) {
      setTourActive(false);
      return;
    }
    setSelectedID(next.id);
  }

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
        <>
          <StatePanel
            state="empty"
            detail="No executable guided demonstrations are installed in this build."
          />
          <WhyNothingPanel
            reason="A demonstration is shown only when this build can execute the real strategy and shared pipeline. This page never substitutes a static illustration."
            nextAction="Review supported workflows"
            to="/run-lab"
          />
        </>
      )}
      {tourItems.length > 0 && (
        <section
          className={styles.section}
          aria-labelledby="all-strategies-tour"
        >
          <h2 id="all-strategies-tour">All strategies tour</h2>
          <p>
            Run every installed deterministic walkthrough in a guided order.
            Each step is read-only, uses no credentials, and creates no durable
            run.
          </p>
          <button className={styles.button} onClick={startTour} type="button">
            Start tour
          </button>
        </section>
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
              onClick={() => selectWalkthrough(demonstration.id)}
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
          {tourStepActive && (
            <div className={styles.notice} aria-live="polite">
              <h3>
                Tour step {selectedTourIndex + 1} of {tourItems.length}
              </h3>
              <p>
                {selectedTourIndex + 1 < tourItems.length
                  ? "Review this evidence, then continue to the next installed strategy."
                  : "You have reviewed every installed strategy walkthrough."}
              </p>
              <button
                className={styles.button}
                onClick={continueTour}
                type="button"
              >
                {selectedTourIndex + 1 < tourItems.length
                  ? `Continue to ${tourItems[selectedTourIndex + 1]?.title}`
                  : "Finish tour"}
              </button>
            </div>
          )}
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
          {result.data.advisory_only && result.data.advisory_evidence ? (
            <EvidenceDetails
              summary="This walkthrough is read-only. It does not create a transfer, exchange order, or fill."
              title="Advisory recommendation evidence"
              value={formatCanonical(result.data.advisory_evidence)}
            />
          ) : (
            <>
              <DemonstrationEventEvidence
                label="Accepted shared-pipeline event"
                event={result.data.accepted}
              />
              <DemonstrationEventEvidence
                label="Rejected shared-pipeline event"
                event={result.data.rejected}
              />
            </>
          )}
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
        summary="Canonical virtual portfolio and accounting projection."
        title="Virtual portfolio and accounting"
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
