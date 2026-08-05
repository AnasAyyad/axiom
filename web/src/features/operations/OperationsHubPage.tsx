import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";

import { d1CollectionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { StatusBadge } from "../shared/StatusBadge";
import styles from "../shared/D2.module.css";

const destinations = [
  [
    "Exchange and data health",
    "/exchanges",
    "Public connectivity, books, recorder state, and freshness.",
  ],
  ["Assets", "/assets", "Approved spot universe and screening state."],
  [
    "Orders",
    "/operations/orders",
    "Durable virtual, test, or demo order projections.",
  ],
  ["Fills", "/operations/fills", "Reconciled fill and accounting evidence."],
  [
    "Alerts",
    "/operations/alerts",
    "Current operational alerts and acknowledgement state.",
  ],
  [
    "Incidents",
    "/incidents",
    "Incident severity, timeline, replay inputs, and resolution evidence.",
  ],
  [
    "Audit",
    "/audit",
    "Tamper-evident authentication, command, and evidence access history.",
  ],
  [
    "Report jobs",
    "/operations/reports",
    "On-demand and scheduled report lifecycle.",
  ],
  [
    "Configuration",
    "/operations/configuration",
    "Immutable configuration revisions and activation state.",
  ],
  [
    "Qualification Center",
    "/operations/qualifications",
    "Approved preflight, progress, abort, and verdict workflows.",
  ],
  [
    "Sandbox Operations",
    "/operations/sandbox",
    "Capped Binance Spot Testnet and Bybit Demo controls.",
  ],
  [
    "User access",
    "/operations/users",
    "Owner-administered role assignments and read-only compatibility.",
  ],
] as const;

export function OperationsHubPage() {
  const alerts = useQuery(d1CollectionQuery("alerts"));
  const qualifications = useQuery(d1CollectionQuery("qualifications"));
  return (
    <Page
      title="Operations"
      eyebrow="Health, evidence, access, and incident response"
      description="Start with operational impact and recommended action, then expand sanitized technical evidence."
    >
      {(alerts.isError || qualifications.isError) && (
        <StatePanel
          state="degraded"
          detail="Navigation is available, but one or more operational summaries are partial."
        />
      )}
      <div className={styles.twoColumn}>
        <article className={styles.card}>
          <h2>Active alerts</h2>
          {alerts.isLoading ? (
            <StatePanel state="loading" />
          ) : (
            <p>
              <StatusBadge
                value={`${alerts.data?.items.length ?? 0} visible`}
              />
            </p>
          )}
          <p>
            Open alerts first, acknowledge only when ownership and impact are
            understood.
          </p>
        </article>
        <article className={styles.card}>
          <h2>Qualifications</h2>
          {qualifications.isLoading ? (
            <StatePanel state="loading" />
          ) : (
            <p>
              <StatusBadge
                value={`${qualifications.data?.items.length ?? 0} approved`}
              />
            </p>
          )}
          <p>
            Missing, failed, expired, or aborted evidence blocks later
            certification.
          </p>
        </article>
      </div>
      <div className={styles.cardGrid}>
        {destinations.map(([title, to, description]) => (
          <article className={styles.card} key={to}>
            <h2>{title}</h2>
            <p>{description}</p>
            <Link className={styles.linkButton} to={to}>
              Open {title}
            </Link>
          </article>
        ))}
      </div>
    </Page>
  );
}
