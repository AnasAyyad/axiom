import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import {
  APIError,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../../api/client";
import { d1CollectionQuery, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { ConfirmAction } from "../../components/ConfirmAction";
import { StatePanel } from "../../components/StatePanel";
import { hasAccess } from "../shared/access";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { stringAttribute } from "../strategies/strategyModel";
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
  const canAcknowledge = hasAccess(session.data.user, ["alert.write"]);
  return (
    <Page
      title="Alert Center"
      eyebrow="Impact, ownership, and acknowledgement"
      description="Review current operational impact and correlation evidence before recording an audited acknowledgement."
    >
      {query.data.items.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No alerts are visible to this role."
        />
      ) : (
        <div className={styles.cardGrid}>
          {query.data.items.map((alert) => (
            <AlertCard
              key={alert.id}
              alert={alert}
              canAcknowledge={canAcknowledge}
            />
          ))}
        </div>
      )}
    </Page>
  );
}

function AlertCard({
  alert,
  canAcknowledge,
}: {
  readonly alert: APIModel<"D1Resource">;
  readonly canAcknowledge: boolean;
}) {
  const [reason, setReason] = useState(
    "Operator reviewed impact and accepted alert ownership",
  );
  const queryClient = useQueryClient();
  const mutation = useMutation({
    mutationFn: () =>
      postAPI<"CommandAccepted">(
        `/api/v1/alerts/${encodeURIComponent(alert.id)}/acknowledge`,
        {
          expected_revision: alert.revision,
          reason: reason.trim(),
        } satisfies APIModel<"RevisionCommandRequest">,
        newIdempotencyKey("alert-acknowledge"),
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["d1", "alerts"] }),
  });
  return (
    <article className={styles.card}>
      <div className={styles.cardHeader}>
        <div>
          <h2>{stringAttribute(alert.attributes, "alert_type", alert.id)}</h2>
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
      {canAcknowledge && alert.state !== "acknowledged" && (
        <div className={styles.controlCard}>
          <label className={styles.field}>
            Acknowledgement reason
            <textarea
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </label>
          <ConfirmAction
            trigger={
              <button className={styles.button} type="button">
                Acknowledge alert
              </button>
            }
            title="Acknowledge and accept ownership?"
            description="Acknowledgement is durable and audited; it does not hide or resolve the underlying incident."
            confirmLabel="Acknowledge alert"
            onConfirm={() => mutation.mutate()}
          />
          {mutation.isError && (
            <p className={styles.error} role="alert">
              Acknowledgement was rejected. Refresh the exact revision and
              retry.
            </p>
          )}
        </div>
      )}
      <EvidenceDetails
        summary="Sanitized alert identity and correlation evidence. Delivery routing is completed in D4."
        value={alert}
      />
    </article>
  );
}
