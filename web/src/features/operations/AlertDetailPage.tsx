import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router-dom";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { alertDetailQuery, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { ConfirmAction } from "../../components/ConfirmAction";
import { DataTable } from "../../components/DataTable";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { hasAccess } from "../shared/access";
import styles from "../shared/D2.module.css";

export function AlertDetailPage() {
  const { id = "" } = useParams();
  const query = useQuery(alertDetailQuery(id));
  const session = useQuery(sessionQuery);
  if (query.isLoading || session.isLoading)
    return <StatePanel state="loading" />;
  if (query.isError || session.isError || !query.data || !session.data)
    return <StatePanel state="forbidden" />;
  const canControl = hasAccess(session.data.user, ["alert.write"]);
  return (
    <Page
      title={`Alert ${query.data.id}`}
      eyebrow={`${query.data.severity} · ${query.data.state}`}
      description="Sanitized alert lifecycle, immutable delivery attempts, escalation history, and correlation evidence."
    >
      <div className={styles.rowHeader}>
        <StatusBadge value={query.data.severity} />
        <Link className={styles.linkButton} to="/operations/alerts">
          Back to alerts
        </Link>
      </div>
      <dl className={styles.facts}>
        <div>
          <dt>Reason</dt>
          <dd>{query.data.reason_code}</dd>
        </div>
        <div>
          <dt>Component</dt>
          <dd>{query.data.component}</dd>
        </div>
        <div>
          <dt>Occurrences</dt>
          <dd>{query.data.occurrences}</dd>
        </div>
        <div>
          <dt>Revision</dt>
          <dd>{query.data.revision}</dd>
        </div>
        <div>
          <dt>Correlation</dt>
          <dd>{query.data.correlation_id}</dd>
        </div>
      </dl>
      {canControl && <AlertControls alert={query.data} />}
      <h2>Delivery attempts</h2>
      {query.data.deliveries.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No external delivery attempts were required or recorded."
        />
      ) : (
        <DataTable
          caption="Immutable sanitized delivery attempts"
          rows={query.data.deliveries.map((attempt) => ({
            ...attempt,
            reason_code: attempt.reason_code ?? "none",
            latency_ms: attempt.latency_ms ?? "unavailable",
          }))}
          columns={[
            { key: "started_at", label: "Started UTC" },
            { key: "sink_name", label: "Route" },
            { key: "attempt", label: "Attempt" },
            { key: "state", label: "Outcome" },
            { key: "reason_code", label: "Reason" },
            { key: "latency_ms", label: "Latency ms" },
          ]}
        />
      )}
      <h2>Escalations</h2>
      {query.data.escalations.length === 0 ? (
        <StatePanel state="empty" detail="No manual escalations recorded." />
      ) : (
        <DataTable
          caption="Audited alert escalations"
          rows={query.data.escalations.map((item) => ({ ...item }))}
          columns={[
            { key: "escalated_at", label: "UTC time" },
            { key: "actor_user_id", label: "Actor" },
            { key: "reason", label: "Reason" },
            { key: "revision", label: "Revision" },
          ]}
        />
      )}
      <EvidenceDetails
        summary="Technical alert identity with no endpoint, credential, header, or private exchange payload."
        value={query.data}
      />
    </Page>
  );
}

function AlertControls({ alert }: { readonly alert: APIModel<"AlertDetail"> }) {
  const client = useQueryClient();
  const [reason, setReason] = useState(
    "Operator reviewed impact and accepted alert ownership",
  );
  const command = useMutation({
    mutationFn: (action: "acknowledge" | "escalate") =>
      postAPI<"CommandAccepted">(
        `/api/v1/alerts/${encodeURIComponent(alert.id)}/${action}`,
        {
          expected_revision: alert.revision,
          reason: reason.trim(),
        } satisfies APIModel<"RevisionCommandRequest">,
        newIdempotencyKey(`alert-${action}`),
      ),
    onSuccess: () =>
      client.invalidateQueries({ queryKey: ["alert", alert.id] }),
  });
  return (
    <section className={styles.controlCard} aria-label="Alert controls">
      <h2>Operator action</h2>
      <label className={styles.field}>
        Reason
        <textarea
          minLength={8}
          value={reason}
          onChange={(event) => setReason(event.target.value)}
        />
      </label>
      <div className={styles.actions}>
        {alert.state !== "acknowledged" && (
          <ConfirmAction
            trigger={
              <button
                className={styles.button}
                type="button"
                disabled={reason.trim().length < 8}
              >
                Acknowledge
              </button>
            }
            title="Acknowledge and accept ownership?"
            description="Acknowledgement is durable and does not hide or resolve the underlying incident."
            confirmLabel="Acknowledge alert"
            onConfirm={() => command.mutate("acknowledge")}
          />
        )}
        <ConfirmAction
          trigger={
            <button
              className={styles.danger}
              type="button"
              disabled={reason.trim().length < 8}
            >
              Escalate
            </button>
          }
          title="Escalate this alert?"
          description="Escalation records the actor, reason, time, and exact alert revision."
          confirmLabel="Escalate alert"
          onConfirm={() => command.mutate("escalate")}
        />
      </div>
      {command.isError && (
        <p className={styles.error} role="alert">
          Action rejected. Refresh the exact alert revision and retry.
        </p>
      )}
    </section>
  );
}
