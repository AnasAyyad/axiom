import { Link } from "react-router";

import { StatePanel } from "../../components/StatePanel";
import shared from "../shared/ConsoleSurface.module.css";
import { StatusBadge } from "../shared/StatusBadge";
import styles from "./StrategyEvaluationPage.module.css";
import { Metric, Progress } from "./StrategyEvaluationPrimitives";
import type { Campaign } from "./StrategyEvaluationTypes";
import {
  formatBytes,
  formatDuration,
  formatLabel,
  formatTime,
} from "./StrategyEvaluationView";

export function CampaignHistory({
  campaigns,
  selectedID,
}: {
  readonly campaigns: readonly Campaign[];
  readonly selectedID: string;
}) {
  return (
    <nav className={styles.history} aria-label="Evaluation campaign history">
      {campaigns.map((campaign) => (
        <Link
          key={campaign.id}
          to={`/strategy-evaluation/${encodeURIComponent(campaign.id)}`}
          aria-current={campaign.id === selectedID ? "page" : undefined}
        >
          <span>{new Date(campaign.created_at).toLocaleDateString()}</span>
          <StatusBadge value={campaign.state} />
        </Link>
      ))}
    </nav>
  );
}

export function CampaignOverview({
  campaign,
}: {
  readonly campaign: Campaign;
}) {
  const recorded = campaign.recorded_bytes ?? 0;
  const limit = campaign.recording_limit_bytes ?? 214_748_364_800;
  const reason = campaign.reason_code;
  return (
    <section className={shared.section} aria-labelledby="evaluation-overview">
      <div className={shared.rowHeader}>
        <div>
          <h2 id="evaluation-overview">Current campaign</h2>
          <p className={styles.identifier}>{campaign.id}</p>
        </div>
        <StatusBadge value={campaign.state} />
      </div>
      {(reason || campaign.suggested_action) && (
        <StatePanel
          state={campaign.state === "PAUSED_RECOVERABLE" ? "paused" : "blocked"}
          detail={[reason, campaign.suggested_action]
            .filter(Boolean)
            .join(" — ")}
        />
      )}
      <div className={styles.metrics}>
        <Metric
          label="Current stage"
          value={formatLabel(campaign.current_stage ?? "Queued")}
        />
        <Metric
          label="Wall time"
          value={formatDuration(campaign.wall_time_seconds)}
        />
        <Metric
          label="Valid recording"
          value={`${formatDuration(campaign.valid_recording_seconds)} / 72h`}
        />
        <Metric
          label="Valid shadow"
          value={`${formatDuration(campaign.valid_shadow_seconds)} / 7d`}
        />
        <Metric
          label="ETA"
          value={formatDuration(campaign.estimated_remaining_seconds)}
        />
        <Metric
          label="Collection rate"
          value={
            campaign.measured_bytes_per_hour === undefined
              ? "Measuring"
              : `${formatBytes(campaign.measured_bytes_per_hour)}/h`
          }
        />
      </div>
      <Progress
        label={`New recording storage: ${formatBytes(recorded)} of ${formatBytes(limit)}`}
        value={recorded}
        max={limit}
      />
      {campaign.shadow_reserved_bytes !== undefined && (
        <p className={styles.supporting}>
          {formatBytes(campaign.shadow_reserved_bytes)} remains reserved for the
          complete shadow evidence window.
        </p>
      )}
    </section>
  );
}

export function StageTimeline({ campaign }: { readonly campaign: Campaign }) {
  return (
    <section className={shared.section} aria-labelledby="evaluation-stages">
      <h2 id="evaluation-stages">Automatic stages</h2>
      <ol
        className={styles.stages}
        tabIndex={0}
        aria-label="Automatic evaluation stages"
      >
        {(campaign.stages ?? []).map((stage) => {
          const lastAttempt = stage.attempts?.at(-1);
          return (
            <li key={stage.stage} data-state={stage.state}>
              <span>{formatLabel(stage.stage)}</span>
              <StatusBadge value={stage.state} />
              <small>
                {stage.reason_code ??
                  (stage.completed_at
                    ? `Completed ${formatTime(stage.completed_at)}`
                    : stage.started_at
                      ? `Started ${formatTime(stage.started_at)}`
                      : "Waiting for the preceding gate")}
              </small>
              {stage.attempt > 0 && (
                <small>
                  Attempt {stage.attempt}
                  {stage.recoverable_failures > 0
                    ? ` · ${stage.recoverable_failures} consecutive recovery check${stage.recoverable_failures === 1 ? "" : "s"}`
                    : ""}
                </small>
              )}
              {stage.next_retry_at && (
                <small>Automatic retry {formatTime(stage.next_retry_at)}</small>
              )}
              {lastAttempt && lastAttempt.outcome !== "COMPLETED" && (
                <small>
                  Previous attempt preserved: {formatLabel(lastAttempt.outcome)}
                </small>
              )}
            </li>
          );
        })}
      </ol>
    </section>
  );
}

export function ImportProgress({ campaign }: { readonly campaign: Campaign }) {
  const imports = campaign.historical_imports ?? [];
  return (
    <section className={shared.section} aria-labelledby="evaluation-imports">
      <div className={shared.rowHeader}>
        <div>
          <h2 id="evaluation-imports">Historical candle import</h2>
          <p>
            Official Binance and Bybit candles · 2023-08-01 through 2026-07-31
          </p>
        </div>
        <StatusBadge
          value={`${imports.filter((item) => item.state === "COMPLETED").length}/${imports.length || 12} complete`}
        />
      </div>
      {imports.length === 0 ? (
        <StatePanel
          state="empty"
          detail="Import tasks are waiting for campaign execution."
        />
      ) : (
        <div className={styles.progressGrid}>
          {imports.map((item) => {
            const max =
              Date.parse(item.window_end) - Date.parse(item.window_start);
            const value =
              Date.parse(item.checkpoint_time) - Date.parse(item.window_start);
            return (
              <article
                className={shared.card}
                key={`${item.exchange}-${item.instrument}-${item.interval}`}
              >
                <div className={shared.cardHeader}>
                  <h3>
                    {item.exchange} · {item.instrument} · {item.interval}
                  </h3>
                  <StatusBadge value={item.state} />
                </div>
                <Progress
                  label={`${item.row_count.toLocaleString()} candles`}
                  value={value}
                  max={max}
                />
                <p>
                  {formatBytes(item.byte_count)} · {item.gap_count} gaps
                </p>
                {item.reason_code && (
                  <p className={shared.error}>{item.reason_code}</p>
                )}
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}
