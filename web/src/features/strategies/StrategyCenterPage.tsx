import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";

import { APIError } from "../../api/client";
import {
  activityQuery,
  sessionQuery,
  strategiesQuery,
  strategyDetailQuery,
  strategyVersionsQuery,
} from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { emptyActivityFilters } from "../activity/activityModel";
import { StrategyControlPanel } from "./StrategyControlPanel";
import {
  strategyPurpose,
  stringAttribute,
  stringListAttribute,
} from "./strategyModel";
import styles from "../shared/D2.module.css";

export function StrategyCenterPage() {
  const [selectedID, setSelectedID] = useState("");
  const session = useQuery(sessionQuery);
  const summaries = useQuery(strategiesQuery);
  const activeID = selectedID || summaries.data?.items[0]?.id || "";
  const detail = useQuery(strategyDetailQuery(activeID));
  const versions = useQuery(strategyVersionsQuery(activeID));
  const activityFilters = useMemo(
    () => ({ ...emptyActivityFilters, strategy: activeID }),
    [activeID],
  );
  const activity = useQuery(activityQuery("decisions_orders", activityFilters));
  if (session.isLoading || summaries.isLoading)
    return <StatePanel state="loading" />;
  if (
    (session.error instanceof APIError && session.error.status === 403) ||
    (summaries.error instanceof APIError && summaries.error.status === 403)
  )
    return <StatePanel state="forbidden" />;
  if (session.isError || summaries.isError || !session.data || !summaries.data)
    return (
      <StatePanel state="error" detail="Strategy summaries are unavailable." />
    );
  const selectedSummary = summaries.data.items.find(
    (item) => item.id === activeID,
  );
  return (
    <Page
      title="Strategy Center"
      eyebrow="Purpose, evidence, readiness, and control"
      description="Understand what each strategy researches, why it is or is not ready, and which versioned or runtime action your role may take."
    >
      {summaries.data.items.length === 0 ? (
        <StatePanel state="empty" />
      ) : (
        <div className={styles.cardGrid}>
          {summaries.data.items.map((strategy) => (
            <button
              className={styles.card}
              type="button"
              aria-pressed={activeID === strategy.id}
              key={strategy.id}
              onClick={() => setSelectedID(strategy.id)}
            >
              <span className={styles.cardHeader}>
                <strong>{strategy.name}</strong>
                <StatusBadge value={strategy.evidence_role} />
              </span>
              <span>{strategy.family.replaceAll("_", " ")}</span>
              <span>Version {strategy.version}</span>
              <span>{strategy.maturity.replaceAll("_", " ")}</span>
            </button>
          ))}
        </div>
      )}
      {selectedSummary && (detail.isLoading || versions.isLoading) && (
        <StatePanel
          state="loading"
          detail="Loading strategy detail and immutable versions…"
        />
      )}
      {selectedSummary && (detail.isError || versions.isError) && (
        <StatePanel
          state="degraded"
          detail="Summary is available, but detailed readiness or version evidence is partial."
        />
      )}
      {selectedSummary && detail.data && (
        <>
          <section className={styles.twoColumn}>
            <article className={styles.card}>
              <div className={styles.cardHeader}>
                <div>
                  <h2>{selectedSummary.name}</h2>
                  <p>{strategyPurpose(selectedSummary.family)}</p>
                </div>
                <StatusBadge value={detail.data.state} />
              </div>
              <dl className={styles.facts}>
                <div>
                  <dt>Version</dt>
                  <dd>{selectedSummary.version}</dd>
                </div>
                <div>
                  <dt>Maturity</dt>
                  <dd>{selectedSummary.maturity}</dd>
                </div>
                <div>
                  <dt>Supported modes</dt>
                  <dd>{selectedSummary.supported_modes.join(", ")}</dd>
                </div>
                <div>
                  <dt>Confidence</dt>
                  <dd>{selectedSummary.confidence}</dd>
                </div>
                <div>
                  <dt>Viability</dt>
                  <dd>{selectedSummary.viability.replaceAll("_", " ")}</dd>
                </div>
                <div>
                  <dt>Configured</dt>
                  <dd>
                    {stringAttribute(
                      detail.data.attributes,
                      "configured_state",
                    )}
                  </dd>
                </div>
                <div>
                  <dt>Runtime</dt>
                  <dd>
                    {stringAttribute(
                      detail.data.attributes,
                      "runtime_state",
                      detail.data.state,
                    )}
                  </dd>
                </div>
              </dl>
              <p className={styles.notice} role="note">
                {selectedSummary.disclaimer} Viability is separate from platform
                readiness, and no historical, shadow, demo, or testnet result
                proves profitability.
              </p>
            </article>
            <article className={styles.card}>
              <h2>Readiness and provenance</h2>
              {stringListAttribute(
                detail.data.attributes,
                "blocking_prerequisites",
              ).length === 0 ? (
                <p className={styles.success}>
                  No blocking prerequisite is reported in this snapshot.
                </p>
              ) : (
                <ul className={styles.notice}>
                  {stringListAttribute(
                    detail.data.attributes,
                    "blocking_prerequisites",
                  ).map((item) => (
                    <li key={item}>{item.replaceAll("_", " ")}</li>
                  ))}
                </ul>
              )}
              <dl className={styles.facts}>
                <div>
                  <dt>Evidence role</dt>
                  <dd>{selectedSummary.evidence_role}</dd>
                </div>
                <div>
                  <dt>Primary metric</dt>
                  <dd>{selectedSummary.primary_metric ?? "Not established"}</dd>
                </div>
                <div>
                  <dt>Latest detail revision</dt>
                  <dd>{detail.data.revision}</dd>
                </div>
                <div>
                  <dt>Version records</dt>
                  <dd>{versions.data?.items.length ?? "Partial"}</dd>
                </div>
                <div>
                  <dt>Recent decisions</dt>
                  <dd>{activity.data?.items.length ?? "Partial"}</dd>
                </div>
              </dl>
              <EvidenceDetails
                summary="Configuration, immutable version, and source links are server-redacted."
                value={{
                  strategy_id: detail.data.id,
                  correlation_id: detail.data.correlation_id,
                  attributes: detail.data.attributes,
                  versions: versions.data?.items,
                  links: detail.data.links,
                }}
              />
            </article>
          </section>
          <StrategyControlPanel
            strategy={detail.data}
            user={session.data.user}
          />
          <section className={styles.card}>
            <h2>Recent decisions</h2>
            {activity.isError ? (
              <StatePanel
                state="degraded"
                detail="Strategy detail is current, but recent activity is unavailable."
              />
            ) : activity.data?.items.length ? (
              <ul>
                {activity.data.items.slice(0, 8).map((item) => (
                  <li key={item.id}>
                    <strong>{item.reason.summary}</strong> — {item.outcome} at{" "}
                    {item.occurred_at}
                  </li>
                ))}
              </ul>
            ) : (
              <StatePanel
                state="empty"
                detail="No projected decision activity exists for this strategy."
              />
            )}
          </section>
        </>
      )}
    </Page>
  );
}
