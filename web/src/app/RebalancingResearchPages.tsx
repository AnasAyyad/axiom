import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  getAPI,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../api/client";
import { championChallengerQuery, rebalancingQuery } from "../api/queries";
import { StatePanel } from "../components/StatePanel";
import { QualityBadge } from "./MultiExchangeShared";
import { Page } from "./OperationalShared";
import styles from "./Page.module.css";

export function RebalancingCenter() {
  const [selected, setSelected] = useState("");
  const query = useQuery(rebalancingQuery);
  const detail = useQuery({
    queryKey: ["rebalancing", selected],
    queryFn: () =>
      getAPI<"RebalancingDetail">(
        `/api/v1/rebalancing/recommendations/${selected}`,
      ),
    enabled: selected !== "",
  });
  if (query.isLoading) return <StatePanel state="loading" />;
  if (query.isError) return <StatePanel state="degraded" />;
  return (
    <Page
      title="Rebalancing Review"
      eyebrow="Advisory only"
      description="Reviewed routes and manual checklists. Axiom exposes no transfer or execution control."
    >
      <div className={styles.notice} role="status">
        Execution unavailable · operator review required · no transfer controls
      </div>
      <div className={styles.cardGrid}>
        {query.data!.items.map((item) => (
          <article className={styles.card} key={item.id}>
            <div className={styles.cardHeading}>
              <h2>
                {item.source_exchange} → {item.destination_exchange}
              </h2>
              <QualityBadge quality={item.quality} />
            </div>
            <dl className={styles.facts}>
              <div>
                <dt>Asset / quantity</dt>
                <dd>
                  {item.source_asset} · {item.quantity}
                </dd>
              </div>
              <div>
                <dt>Method</dt>
                <dd>{item.method.replaceAll("_", " ")}</dd>
              </div>
              <div>
                <dt>Cost / risk</dt>
                <dd>
                  {item.total_cost} / {item.risk_score}
                </dd>
              </div>
            </dl>
            <button
              type="button"
              className={styles.actionSecondary}
              onClick={() => setSelected(item.id)}
            >
              Review route and checklist
            </button>
          </article>
        ))}
      </div>
      {selected !== "" && (
        <section className={styles.card} aria-live="polite">
          <h2>Manual review evidence</h2>
          {detail.isLoading && <StatePanel state="loading" />}
          {detail.isError && <StatePanel state="error" />}
          {detail.data && (
            <div className={styles.evidenceGrid}>
              <ol className={styles.timeline}>
                {detail.data.route.map((step) => (
                  <li key={step.index}>
                    <strong>
                      {step.role} · {step.from_exchange} → {step.to_exchange}
                    </strong>
                    <span>
                      {step.from_asset} → {step.to_asset} · cost{" "}
                      {step.expected_cost}
                    </span>
                  </li>
                ))}
              </ol>
              <ol className={styles.checklist}>
                {detail.data.checklist.map((step) => (
                  <li key={step.index}>{step.instruction}</li>
                ))}
              </ol>
            </div>
          )}
        </section>
      )}
    </Page>
  );
}

export function ResearchReports() {
  const query = useQuery(championChallengerQuery);
  const [exported, setExported] =
    useState<APIModel<"ReportExportResource"> | null>(null);
  const exportReport = useMutation({
    mutationFn: ({ id, format }: { id: string; format: "json" | "csv" }) =>
      postAPI<"ReportExportResource">(
        `/api/v1/reports/${id}/exports`,
        { format },
        newIdempotencyKey(`report-${format}`),
      ),
    onSuccess: setExported,
  });
  if (query.isLoading) return <StatePanel state="loading" />;
  if (query.isError) return <StatePanel state="degraded" />;
  return (
    <Page
      title="Research Reports"
      eyebrow="Champion / challenger"
      description="Immutable comparison manifests and explicit research-only dispositions."
    >
      {query.data!.items.length === 0 ? (
        <StatePanel state="empty" />
      ) : (
        <div className={styles.cardGrid}>
          {query.data!.items.map((report) => (
            <article className={styles.card} key={report.id}>
              <span className={styles.eyebrow}>{report.disposition}</span>
              <h2>
                {report.champion_strategy_version} vs{" "}
                {report.challenger_strategy_version}
              </h2>
              <dl className={styles.facts}>
                <div>
                  <dt>Confidence</dt>
                  <dd>{report.confidence}</dd>
                </div>
                <div>
                  <dt>Viability</dt>
                  <dd>{report.viability}</dd>
                </div>
                <div>
                  <dt>Manifest</dt>
                  <dd className={styles.hash}>{report.manifest_hash}</dd>
                </div>
              </dl>
              <p className={styles.disclaimer}>{report.disclaimer}</p>
              <div className={styles.actions}>
                {(["json", "csv"] as const).map((format) => (
                  <button
                    key={format}
                    type="button"
                    className={styles.actionSecondary}
                    disabled={exportReport.isPending}
                    onClick={() =>
                      exportReport.mutate({ id: report.id, format })
                    }
                  >
                    Create {format.toUpperCase()} export
                  </button>
                ))}
              </div>
            </article>
          ))}
        </div>
      )}
      {exported && (
        <section className={styles.card} aria-live="polite">
          <h2>Immutable export</h2>
          <p>
            {exported.content_type} · {exported.payload_hash}
          </p>
          <pre className={styles.canonical}>{exported.content}</pre>
        </section>
      )}
    </Page>
  );
}
