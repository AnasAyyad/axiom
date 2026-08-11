import { StatePanel } from "../../components/StatePanel";
import shared from "../shared/ConsoleSurface.module.css";
import { StatusBadge } from "../shared/StatusBadge";
import styles from "./StrategyEvaluationPage.module.css";
import { Fact, Progress } from "./StrategyEvaluationPrimitives";
import type { Campaign, Member } from "./StrategyEvaluationTypes";
import {
  formatBytes,
  formatDuration,
  formatLabel,
  formatTime,
  formatUSDT,
  memberMetric,
  summarizeMembers,
} from "./StrategyEvaluationView";

export function Coverage({ campaign }: { readonly campaign: Campaign }) {
  const coverage = campaign.coverage ?? [];
  return (
    <section className={shared.section} aria-labelledby="evaluation-coverage">
      <h2 id="evaluation-coverage">Existing-data audit and coverage</h2>
      {coverage.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No completed audit findings are available yet."
        />
      ) : (
        <div
          className={shared.tableFrame}
          role="region"
          aria-label="Existing dataset audit table"
          tabIndex={0}
        >
          <table>
            <caption>
              Immutable datasets are classified, never silently repaired or
              deleted.
            </caption>
            <thead>
              <tr>
                <th>Dataset</th>
                <th>Exchange</th>
                <th>Instrument</th>
                <th>Eligibility</th>
                <th>Segments</th>
                <th>Bytes</th>
                <th>Gaps</th>
                <th>Duplicates</th>
                <th>Reason</th>
              </tr>
            </thead>
            <tbody>
              {coverage.map((item) => (
                <tr key={item.dataset_id}>
                  <td className={shared.inlineCode}>{item.dataset_id}</td>
                  <td>{item.exchange ?? "Mixed"}</td>
                  <td>{item.instrument ?? "Mixed"}</td>
                  <td>
                    <StatusBadge value={item.eligibility} />
                  </td>
                  <td>{item.segment_count}</td>
                  <td>{formatBytes(item.byte_count)}</td>
                  <td>{item.gap_count}</td>
                  <td>{item.duplicate_count}</td>
                  <td>{item.reason_code}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

export function FeedHealth({ campaign }: { readonly campaign: Campaign }) {
  const feeds = campaign.feed_health ?? [];
  return (
    <section className={shared.section} aria-labelledby="evaluation-feeds">
      <h2 id="evaluation-feeds">Fresh recorder qualification</h2>
      <Progress
        label={`${formatDuration(campaign.valid_recording_seconds)} valid across all six feeds`}
        value={campaign.valid_recording_seconds ?? 0}
        max={72 * 60 * 60}
      />
      {feeds.length === 0 ? (
        <StatePanel
          state="empty"
          detail="Fresh simultaneous feed observations have not started."
        />
      ) : (
        <div className={styles.feedGrid}>
          {feeds.map((feed) => (
            <article
              className={shared.card}
              key={`${feed.exchange}-${feed.instrument}`}
            >
              <div className={shared.cardHeader}>
                <h3>
                  {feed.exchange} · {feed.instrument}
                </h3>
                <StatusBadge value={feed.eligible ? "eligible" : "paused"} />
              </div>
              <dl className={shared.facts}>
                <Fact
                  label="Book"
                  value={feed.book_fresh ? "Fresh" : "Stale"}
                />
                <Fact
                  label="Clock"
                  value={feed.clock_eligible ? "Eligible" : "Unsafe"}
                />
                <Fact
                  label="Latest event"
                  value={formatTime(feed.latest_event_at)}
                />
                <Fact
                  label="Messages"
                  value={feed.message_count.toLocaleString()}
                />
                <Fact
                  label="Queue drops"
                  value={String(feed.queue_drop_count)}
                />
                <Fact
                  label="Gaps / decode"
                  value={`${feed.gap_count} / ${feed.decoder_error_count}`}
                />
              </dl>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

export function Matrix({
  campaign,
  summary,
}: {
  readonly campaign: Campaign;
  readonly summary: ReturnType<typeof summarizeMembers>;
}) {
  const members = campaign.matrix ?? [];
  return (
    <section className={shared.section} aria-labelledby="evaluation-matrix">
      <div className={shared.rowHeader}>
        <div>
          <h2 id="evaluation-matrix">Backtest and replay matrix</h2>
          <p>
            All capital levels, deterministic repeats, and 1.5×/2× cost stress
            remain visible.
          </p>
        </div>
        <StatusBadge
          value={`${summary.terminal}/${summary.total || 136} terminal`}
        />
      </div>
      <Progress
        label={`${summary.succeeded} succeeded · ${summary.failed} failed · ${summary.running} active`}
        value={summary.terminal}
        max={summary.total || 136}
      />
      {members.length === 0 ? (
        <StatePanel
          state="empty"
          detail="The server has not scheduled the offline matrix yet."
        />
      ) : (
        <div
          className={shared.tableFrame}
          role="region"
          aria-label="Backtest and replay matrix table"
          tabIndex={0}
        >
          <table>
            <caption>
              One strategy failure does not stop unrelated members.
            </caption>
            <thead>
              <tr>
                <th>Strategy</th>
                <th>Configuration</th>
                <th>Mode</th>
                <th>Capital</th>
                <th>Repeat</th>
                <th>Cost</th>
                <th>State</th>
                <th>Verdict</th>
                <th>Net</th>
                <th>Trades</th>
              </tr>
            </thead>
            <tbody>
              {members.map((member) => (
                <MemberRow key={member.id} member={member} />
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function MemberRow({ member }: { readonly member: Member }) {
  return (
    <tr>
      <td>{formatLabel(member.strategy)}</td>
      <td>{member.configuration}</td>
      <td>{member.mode}</td>
      <td>{formatUSDT(member.capital_micros)}</td>
      <td>{member.repeat_ordinal}</td>
      <td>{(member.cost_stress_bps / 10_000).toFixed(1)}×</td>
      <td>
        <StatusBadge value={member.state} />
      </td>
      <td>{member.verdict ?? "—"}</td>
      <td>{memberMetric(member, "net_result_micros")}</td>
      <td>{memberMetric(member, "trade_count")}</td>
    </tr>
  );
}
