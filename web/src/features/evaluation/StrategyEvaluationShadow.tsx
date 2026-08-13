import type { APIModel } from "../../api/client";
import { StatePanel } from "../../components/StatePanel";
import shared from "../shared/ConsoleSurface.module.css";
import { StatusBadge } from "../shared/StatusBadge";
import styles from "./StrategyEvaluationPage.module.css";
import { Fact, Metric, Progress } from "./StrategyEvaluationPrimitives";
import type { Campaign } from "./StrategyEvaluationTypes";
import {
  formatDuration,
  formatLabel,
  formatTime,
  formatUSDT,
  memberFunnel,
  memberMetric,
} from "./StrategyEvaluationView";

export function Shadow({ campaign }: { readonly campaign: Campaign }) {
  const shadow = campaign.shadow;
  return (
    <section className={shared.section} aria-labelledby="evaluation-shadow">
      <h2 id="evaluation-shadow">Combined seven-valid-day shadow</h2>
      {!shadow ? (
        <StatePanel
          state="empty"
          detail="Candidate selection has not opened the combined shadow runtime."
        />
      ) : (
        <>
          <div className={styles.metrics}>
            <Metric label="State" value={formatLabel(shadow.state)} />
            <Metric
              label="Valid time"
              value={`${formatDuration(shadow.valid_seconds)} / 7d`}
            />
            <Metric
              label="Shared capital"
              value={formatUSDT(shadow.shared_capital_micros)}
            />
            <Metric
              label="Protected reserve"
              value={formatUSDT(shadow.protected_reserve_micros)}
            />
            <Metric
              label="Per-member ceiling"
              value={formatUSDT(shadow.member_ceiling_micros)}
            />
            <Metric
              label="Input ordinal"
              value={shadow.last_processed_ordinal.toLocaleString()}
            />
          </div>
          <Progress
            label="Only healthy, recorded intervals count"
            value={shadow.valid_seconds}
            max={7 * 24 * 60 * 60}
          />
          {shadow.reason_code && (
            <StatePanel state="blocked" detail={shadow.reason_code} />
          )}
          <div className={styles.progressGrid}>
            {shadow.members.map((member) => (
              <article className={shared.card} key={member.id}>
                <div className={shared.cardHeader}>
                  <h3>{formatLabel(member.strategy)}</h3>
                  <StatusBadge value={member.state} />
                </div>
                <dl className={shared.facts}>
                  <Fact
                    label="Net after costs"
                    value={memberMetric(member, "net_result_micros")}
                  />
                  <Fact
                    label="Trades"
                    value={memberMetric(member, "trade_count")}
                  />
                  <Fact
                    label="Drawdown"
                    value={memberMetric(member, "maximum_drawdown_bps", " bps")}
                  />
                  <Fact
                    label="Opportunity funnel"
                    value={memberFunnel(member)}
                  />
                </dl>
              </article>
            ))}
          </div>
        </>
      )}
    </section>
  );
}

export function EventTimeline({
  items,
}: {
  readonly items: readonly APIModel<"EvaluationCampaignEvent">[];
}) {
  if (items.length === 0)
    return (
      <StatePanel
        state="empty"
        detail="No timeline events have been recorded."
      />
    );
  return (
    <ol className={styles.events}>
      {[...items].reverse().map((event) => (
        <li key={event.ordinal}>
          <div>
            <strong>{formatLabel(event.event_type)}</strong>
            <time>{formatTime(event.occurred_at)}</time>
          </div>
          <p>
            {event.summary ??
              event.reason_code ??
              "Durable state transition recorded."}
          </p>
          {event.stage && <small>{formatLabel(event.stage)}</small>}
        </li>
      ))}
    </ol>
  );
}
