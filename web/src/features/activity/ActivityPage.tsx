import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router";

import { APIError, type APIModel } from "../../api/client";
import { activityQuery, sessionQuery } from "../../api/queries";
import { StatePanel } from "../../components/StatePanel";
import { Page } from "../../app/OperationalShared";
import { hasAccess } from "../shared/access";
import { StatusBadge } from "../shared/StatusBadge";
import { ActivityDetailPanel } from "./ActivityDetailPanel";
import { ActivityFilters } from "./ActivityFilters";
import { emptyActivityFilters, localDateTimeToUTC } from "./activityModel";
import styles from "../shared/D2.module.css";

interface ActivityPageProps {
  readonly view: "decisions_orders" | "system_events";
}

export function ActivityPage({ view }: ActivityPageProps) {
  const [searchParameters] = useSearchParams();
  const [filters, setFilters] = useState(() => ({
    ...emptyActivityFilters,
    correlation_id: searchParameters.get("correlation_id") ?? "",
  }));
  const [selected, setSelected] = useState<APIModel<"ActivityResource">>();
  const session = useQuery(sessionQuery);
  const normalizedFilters = useMemo(
    () => ({
      ...filters,
      from: localDateTimeToUTC(filters.from),
      to: localDateTimeToUTC(filters.to),
    }),
    [filters],
  );
  const query = useQuery(activityQuery(view, normalizedFilters));
  if (session.isLoading || query.isLoading)
    return <StatePanel state="loading" />;
  if (
    (session.error instanceof APIError && session.error.status === 403) ||
    (query.error instanceof APIError && query.error.status === 403)
  )
    return <StatePanel state="forbidden" />;
  if (session.isError || query.isError || !session.data || !query.data)
    return (
      <StatePanel
        state="error"
        detail="Activity could not be loaded from the authoritative projection."
      />
    );
  const canExport = hasAccess(session.data.user, ["artifacts.read"]);
  return (
    <Page
      title={
        view === "decisions_orders" ? "Decisions & Orders" : "System Events"
      }
      eyebrow={
        view === "decisions_orders"
          ? "Explain every outcome"
          : "Restricted operational evidence"
      }
      description={
        view === "decisions_orders"
          ? "Trace buys, sells, fills, cancellations, skips, rejects, and no-action decisions from reason to durable evidence."
          : "Review sanitized connectivity, lifecycle, storage, alert, and incident events without exposing arbitrary logs or private payloads."
      }
    >
      <nav className={styles.tabs} aria-label="Activity views">
        <Link
          aria-current={view === "decisions_orders" ? "page" : undefined}
          to="/activity/decisions-orders"
        >
          Decisions & Orders
        </Link>
        <Link
          aria-current={view === "system_events" ? "page" : undefined}
          to="/activity/system-events"
        >
          System Events
        </Link>
      </nav>
      <ActivityFilters
        value={filters}
        onChange={setFilters}
        onReset={() => setFilters(emptyActivityFilters)}
      />
      {query.isFetching && !query.isLoading && (
        <StatePanel
          state="stale"
          detail="Showing the last snapshot while the filtered result refreshes."
        />
      )}
      {query.data.items.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No activity matches the selected filters."
        />
      ) : (
        <div className={styles.tableFrame} tabIndex={0}>
          <table>
            <caption>Newest immutable activity first</caption>
            <thead>
              <tr>
                <th scope="col">Time</th>
                <th scope="col">Outcome</th>
                <th scope="col">Reason</th>
                <th scope="col">Strategy / instrument</th>
                <th scope="col">Mode</th>
                <th scope="col">Correlation</th>
              </tr>
            </thead>
            <tbody>
              {query.data.items.map((item) => (
                <tr key={item.id}>
                  <td>{item.occurred_at}</td>
                  <td>
                    <StatusBadge value={item.outcome} />
                  </td>
                  <td>
                    <button
                      className={styles.tableButton}
                      type="button"
                      onClick={() => setSelected(item)}
                    >
                      {item.reason.summary}
                    </button>
                    <br />
                    <small>{item.reason.code}</small>
                  </td>
                  <td>
                    {item.strategy_id ?? "—"} / {item.instrument_id ?? "—"}
                  </td>
                  <td>{item.mode ?? "—"}</td>
                  <td>
                    <button
                      className={styles.tableButton}
                      type="button"
                      onClick={() =>
                        setFilters({
                          ...filters,
                          correlation_id: item.correlation_id,
                        })
                      }
                    >
                      {item.correlation_id}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {selected && (
        <ActivityDetailPanel
          activity={selected}
          canExport={canExport}
          onCorrelation={(correlation_id) =>
            setFilters({ ...filters, correlation_id })
          }
        />
      )}
      {query.data.has_more && (
        <p className={styles.heroNote}>
          More records exist. Narrow the stable filters or continue from the
          server cursor in an authorized export workflow.
        </p>
      )}
    </Page>
  );
}
