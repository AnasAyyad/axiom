import { apiErrorDetail } from "../../api/client";
import { StatePanel } from "../../components/StatePanel";
import shared from "../shared/ConsoleSurface.module.css";
import { StatusBadge } from "../shared/StatusBadge";
import styles from "./StrategyEvaluationPage.module.css";
import { Metric } from "./StrategyEvaluationPrimitives";
import type { Report } from "./StrategyEvaluationTypes";
import {
  downloadReport,
  formatLabel,
  formatTime,
  isRecord,
  reportMemberNet,
} from "./StrategyEvaluationView";

export function ReportPanel({
  report,
  error,
  loading,
}: {
  readonly report: Report | undefined;
  readonly error: unknown;
  readonly loading: boolean;
}) {
  return (
    <section className={shared.section} aria-labelledby="evaluation-report">
      <div className={shared.rowHeader}>
        <div>
          <h2 id="evaluation-report">Final or partial report</h2>
          <p>
            Every terminal path preserves completed work, exact reason, hashes,
            and the next safe action.
          </p>
        </div>
        {report && <StatusBadge value={report.verdict ?? report.state} />}
      </div>
      {loading && <StatePanel state="loading" />}
      {error !== null && error !== undefined && (
        <StatePanel
          state="partial"
          detail={apiErrorDetail(
            error,
            "Campaign progress remains available, but the report could not be refreshed.",
          )}
        />
      )}
      {report?.state === "not_ready" && (
        <StatePanel
          state="empty"
          detail="The report will appear automatically at completion, block, cancellation, or early failure."
        />
      )}
      {report && report.state !== "not_ready" && (
        <>
          <div className={styles.metrics}>
            <Metric label="Report state" value={formatLabel(report.state)} />
            <Metric label="Verdict" value={report.verdict ?? "Not assigned"} />
            <Metric label="Generated" value={formatTime(report.generated_at)} />
            <Metric label="Hash" value={report.report_hash ?? "Unavailable"} />
          </div>
          <p className={shared.notice}>{report.summary}</p>
          <ComparisonTable content={report.content} />
          <div className={shared.actions}>
            <button
              className={shared.secondary}
              type="button"
              onClick={() => downloadReport(report, "json")}
            >
              Download JSON
            </button>
            <button
              className={shared.secondary}
              type="button"
              onClick={() => downloadReport(report, "html")}
            >
              Download HTML
            </button>
          </div>
        </>
      )}
    </section>
  );
}

function ComparisonTable({
  content,
}: {
  readonly content: Record<string, unknown> | undefined;
}) {
  const members = Array.isArray(content?.members)
    ? content.members.filter(isRecord)
    : [];
  const locks = Array.isArray(content?.candidate_locks)
    ? content.candidate_locks.filter(isRecord)
    : [];
  const rows = [
    "trend-following",
    "mean-reversion",
    "triangular-arbitrage",
    "cross-exchange-arbitrage",
    "inventory-rebalancing",
  ].map((strategy) => {
    const lock = locks.find(
      (value) => value.strategy === strategy && value.state === "SELECTED",
    );
    const configuration =
      typeof lock?.configuration_key === "string"
        ? lock.configuration_key
        : undefined;
    const selected = members.filter(
      (member) =>
        member.strategy === strategy &&
        (configuration === undefined || member.configuration === configuration),
    );
    return {
      strategy,
      backtest: selected.find(
        (m) =>
          m.mode === "backtest" &&
          m.repeat_ordinal === 0 &&
          m.cost_stress_bps === 10_000,
      ),
      replay:
        selected.find(
          (m) =>
            m.mode === "replay" &&
            m.repeat_ordinal === 2 &&
            m.cost_stress_bps === 10_000,
        ) ?? selected.find((m) => m.mode === "replay"),
      shadow: selected.find((m) => m.mode === "shadow"),
      verdict:
        selected.find(
          (m) => m.mode === "shadow" && typeof m.verdict === "string",
        )?.verdict ??
        selected.find((m) => typeof m.verdict === "string")?.verdict,
    };
  });
  if (members.length === 0) return null;
  return (
    <div
      className={shared.tableFrame}
      role="region"
      aria-label="Backtest replay and shadow comparison table"
      tabIndex={0}
    >
      <table>
        <caption>
          Backtest versus replay versus combined-shadow evidence
        </caption>
        <thead>
          <tr>
            <th>Strategy</th>
            <th>Backtest net</th>
            <th>Replay net</th>
            <th>Shadow net</th>
            <th>Verdict</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.strategy}>
              <td>{formatLabel(row.strategy)}</td>
              <td>{reportMemberNet(row.backtest)}</td>
              <td>{reportMemberNet(row.replay)}</td>
              <td>{reportMemberNet(row.shadow)}</td>
              <td>{String(row.verdict ?? "Pending")}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
