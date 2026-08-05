import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";

import { APIError } from "../../api/client";
import { d1CollectionQuery, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { hasAccess } from "../shared/access";
import { stringAttribute } from "../strategies/strategyModel";
import { AlertRoutePanel } from "./AlertRoutePanel";
import styles from "../shared/D2.module.css";

export function AlertCenterPage() {
  const session = useQuery(sessionQuery);
  const query = useQuery(d1CollectionQuery("alerts"));
  if (session.isLoading || query.isLoading)
    return <StatePanel state="loading" />;
  if (
    (session.error instanceof APIError && session.error.status === 403) ||
    (query.error instanceof APIError && query.error.status === 403)
  )
    return <StatePanel state="forbidden" />;
  if (session.isError || query.isError || !session.data || !query.data)
    return <StatePanel state="error" detail="Alert state is unavailable." />;
  const canControl = hasAccess(session.data.user, ["alert.write"]);
  return (
    <Page
      title="Alert Center"
      eyebrow="Routing, acknowledgement, and escalation"
      description="Review operational impact, ownership, sanitized delivery attempts, and failure handling before acting."
    >
      {query.isFetching && (
        <StatePanel
          state="stale"
          detail="Showing the prior alert snapshot while authoritative state refreshes."
        />
      )}
      {query.data.items.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No alerts are visible to this role."
        />
      ) : (
        <div className={styles.cardGrid}>
          {query.data.items.map((alert) => (
            <article className={styles.card} key={alert.id}>
              <div className={styles.cardHeader}>
                <div>
                  <h2>
                    {stringAttribute(alert.attributes, "alert_type", alert.id)}
                  </h2>
                  <p>{alert.reason?.summary ?? "Operational alert recorded"}</p>
                </div>
                <StatusBadge value={alert.state} />
              </div>
              {alert.reason && (
                <div
                  className={styles.reasonCard}
                  data-severity={alert.reason.severity}
                >
                  <h3>{alert.reason.explanation}</h3>
                  <p>Recommended action: {alert.reason.suggested_action}</p>
                </div>
              )}
              <dl className={styles.facts}>
                <div>
                  <dt>Revision</dt>
                  <dd>{alert.revision}</dd>
                </div>
                <div>
                  <dt>Correlation</dt>
                  <dd>{alert.correlation_id}</dd>
                </div>
              </dl>
              <Link
                className={styles.linkButton}
                to={`/operations/alerts/${encodeURIComponent(alert.id)}`}
              >
                Open delivery and escalation evidence
              </Link>
              <EvidenceDetails
                summary="Sanitized alert identity and correlation evidence."
                value={alert}
              />
            </article>
          ))}
        </div>
      )}
      <AlertRoutePanel canTest={canControl} />
    </Page>
  );
}
