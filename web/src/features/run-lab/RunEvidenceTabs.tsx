import type { APIModel } from "../../api/client";
import styles from "../shared/ConsoleSurface.module.css";
import { RunPortfolioRiskTabs } from "./RunPortfolioRiskTabs";
import {
  exchangeLabel,
  readableMachineValue,
  type EvidenceTab,
} from "./runEvidencePresentation";

interface OutputQueryState {
  readonly isLoading: boolean;
  readonly isError: boolean;
  readonly data: APIModel<"RunOutputPage"> | undefined;
}

interface RunEvidenceTabsProps {
  readonly activeTab: EvidenceTab;
  readonly run: APIModel<"RunResource">;
  readonly outputs: readonly OutputQueryState[];
  readonly portfolio: APIModel<"RunPortfolioProjection"> | undefined;
  readonly risk: APIModel<"RunRiskProjection"> | undefined;
  readonly evidence: APIModel<"RunEvidence"> | undefined;
}

const outputViews = [
  ["timeline", "Timeline"],
  ["decisions", "Decisions"],
  ["orders", "Orders"],
  ["fills", "Fills"],
] as const;

function runTabLabel(tab: EvidenceTab) {
  switch (tab) {
    case "timeline":
      return "Timeline";
    case "decisions":
      return "Decisions";
    case "orders":
      return "Orders & Fills";
    default:
      return tab;
  }
}

function runOutputSummary(item: APIModel<"RunOutput">) {
  try {
    const payload = JSON.parse(item.canonical_payload) as Record<
      string,
      unknown
    >;
    switch (payload.event_type) {
      case "strategy_evaluation":
        return `${exchangeLabel(payload.exchange)}: ${readableMachineValue(payload.state)} — ${readableMachineValue(payload.reason)}.`;
      case "strategy_decision":
        return `${exchangeLabel(payload.exchange)} recorded a strategy decision for ${readableMachineValue(payload.instrument)}${payload.plan_created ? " and created a capped plan" : " without creating an order"}.`;
      case "sandbox_order":
        return `${exchangeLabel(payload.exchange)} ${readableMachineValue(payload.side)} order ${readableMachineValue(payload.state)} — ${String(payload.quantity ?? "unknown")} ${readableMachineValue(payload.instrument)} at ${String(payload.limit_price ?? "unknown")}.`;
      case "sandbox_fill":
        return `${exchangeLabel(payload.exchange)} recorded a ${readableMachineValue(payload.side)} fill of ${String(payload.quantity ?? "unknown")} ${readableMachineValue(payload.instrument)} at ${String(payload.price ?? "unknown")}; journal ${readableMachineValue(payload.journal_state)}.`;
      case "risk_valuation":
        return `${exchangeLabel(payload.exchange)} central-risk valuation recorded total P&L ${String(payload.strategy_total_pnl ?? "unknown")} with ${String(payload.open_orders ?? "unknown")} open order(s).`;
      default:
        if (typeof payload.outcome === "string")
          return `Recorded ${readableMachineValue(payload.outcome)} outcome.`;
        if (typeof payload.action === "string")
          return `Recorded ${readableMachineValue(payload.action)} decision.`;
    }
  } catch {
    return "The immutable record is available in advanced details.";
  }
  return `Recorded ${readableMachineValue(item.kind)} evidence.`;
}

export function RunEvidenceTabs({
  activeTab,
  run,
  outputs,
  portfolio,
  risk,
  evidence,
}: RunEvidenceTabsProps) {
  const outputIndexes =
    activeTab === "timeline"
      ? [0]
      : activeTab === "decisions"
        ? [1]
        : activeTab === "orders"
          ? [2, 3]
          : [];
  return (
    <>
      {outputIndexes.length > 0 && (
        <section
          aria-labelledby={`run-tab-control-${activeTab}`}
          className={styles.section}
          id={`run-tab-${activeTab}`}
          role="tabpanel"
        >
          <h2 id="run-records">
            {activeTab === "orders"
              ? "Orders and fills"
              : runTabLabel(activeTab)}
          </h2>
          <p>
            Empty collections mean this run has not recorded that kind of event.
            They do not mean an event was inferred or skipped.
          </p>
          <div className={styles.cardGrid}>
            {outputIndexes.map((index) => {
              const [view, label] = outputViews[index]!;
              const result = outputs[index];
              return (
                <article className={styles.card} key={view}>
                  <h3>{label}</h3>
                  {result?.isLoading && <p>Loading recorded evidence…</p>}
                  {result?.isError && <p>Recorded evidence is unavailable.</p>}
                  {result?.data && (
                    <p>{result.data.items.length} recorded item(s).</p>
                  )}
                  {result?.data && result.data.items.length > 0 && (
                    <ol>
                      {result.data.items.slice(0, 10).map((item, index) => (
                        <li key={`${item.kind}-${item.ordinal}-${index}`}>
                          <p>{runOutputSummary(item)}</p>
                          <details>
                            <summary>Advanced immutable identity</summary>
                            Event {item.ordinal} · content hash{" "}
                            {item.content_hash.slice(0, 12)}…
                          </details>
                        </li>
                      ))}
                    </ol>
                  )}
                </article>
              );
            })}
          </div>
        </section>
      )}
      <RunPortfolioRiskTabs
        activeTab={activeTab}
        portfolio={portfolio}
        risk={risk}
      />
      {activeTab === "data" && (
        <section
          aria-labelledby="run-tab-control-data"
          className={styles.card}
          id="run-tab-data"
          role="tabpanel"
        >
          <h2>Data and models</h2>
          <p>
            This run uses immutable server-selected inputs. Dataset coverage,
            model identity, and source/build provenance appear only after the
            worker records them; the console never substitutes current global
            state for a missing run record.
          </p>
          <dl className={styles.facts}>
            <div>
              <dt>Strategy version</dt>
              <dd>{run.strategy_version}</dd>
            </div>
            <div>
              <dt>Environment</dt>
              <dd>{run.environment.replaceAll("_", " ")}</dd>
            </div>
          </dl>
        </section>
      )}
      {activeTab === "evidence" && (
        <section
          aria-labelledby="run-tab-control-evidence"
          className={styles.card}
          id="run-tab-evidence"
          role="tabpanel"
        >
          <h2>Evidence</h2>
          {evidence?.state === "recorded" ? (
            <details>
              <summary>Advanced reproducibility identity</summary>
              <dl className={styles.facts}>
                <div>
                  <dt>Manifest</dt>
                  <dd>{evidence.manifest_hash}</dd>
                </div>
                <div>
                  <dt>Source commit</dt>
                  <dd>{evidence.source_commit}</dd>
                </div>
                <div>
                  <dt>Confidence tier</dt>
                  <dd>{evidence.confidence_tier}</dd>
                </div>
              </dl>
            </details>
          ) : (
            <p>
              No immutable evidence manifest has been recorded for this run yet.
            </p>
          )}
        </section>
      )}
    </>
  );
}
