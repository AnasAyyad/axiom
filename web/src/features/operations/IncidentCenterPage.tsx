import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { incidentsQueryForState, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { StatusBadge } from "../shared/StatusBadge";
import { hasAccess } from "../shared/access";
import styles from "../shared/D2.module.css";

export function IncidentCenterPage() {
  const [state, setState] = useState("");
  const incidents = useQuery(incidentsQueryForState(state));
  const session = useQuery(sessionQuery);
  if (incidents.isLoading || session.isLoading)
    return <StatePanel state="loading" />;
  if (incidents.isError || session.isError || !incidents.data || !session.data)
    return <StatePanel state="forbidden" />;
  const canWrite = hasAccess(session.data.user, ["incident.write"]);
  return (
    <Page
      title="Incident Center"
      eyebrow="Ownership, timeline, replay, and resolution"
      description="Open and manage operational incidents through hash-linked timelines, correlated evidence, deterministic replay inputs, and explicit resolution proof."
    >
      <div className={styles.twoColumn}>
        <label className={styles.field}>
          Incident state
          <select
            value={state}
            onChange={(event) => setState(event.target.value)}
          >
            <option value="">All states</option>
            <option value="open">Open</option>
            <option value="acknowledged">Acknowledged</option>
            <option value="resolved">Resolved</option>
          </select>
        </label>
      </div>
      {canWrite && <CreateIncident ownerID={session.data.user.id} />}
      {incidents.isFetching && (
        <StatePanel
          state="stale"
          detail="Showing the prior incident snapshot while authoritative state refreshes."
        />
      )}
      {incidents.data.items.length === 0 ? (
        <StatePanel state="empty" detail="No incidents match this filter." />
      ) : (
        <div className={styles.cardGrid}>
          {incidents.data.items.map((incident) => (
            <article className={styles.card} key={incident.id}>
              <div className={styles.cardHeader}>
                <h2>{incident.reason_code.replaceAll("_", " ")}</h2>
                <StatusBadge value={incident.severity} />
              </div>
              <dl className={styles.facts}>
                <div>
                  <dt>State</dt>
                  <dd>{incident.state}</dd>
                </div>
                <div>
                  <dt>Owner</dt>
                  <dd>{incident.owner_user_id || "Unassigned"}</dd>
                </div>
                <div>
                  <dt>Updated</dt>
                  <dd>{incident.updated_at}</dd>
                </div>
                <div>
                  <dt>Revision</dt>
                  <dd>{incident.revision}</dd>
                </div>
              </dl>
              <Link
                className={styles.linkButton}
                to={`/incidents/${encodeURIComponent(incident.id)}`}
              >
                Open incident workspace
              </Link>
            </article>
          ))}
        </div>
      )}
    </Page>
  );
}

function CreateIncident({ ownerID }: { readonly ownerID: string }) {
  const client = useQueryClient();
  const [severity, setSeverity] =
    useState<APIModel<"IncidentCreateRequest">["severity"]>("warning");
  const [reasonCode, setReasonCode] = useState(
    "operational_investigation_required",
  );
  const [owner, setOwner] = useState(ownerID);
  const [reason, setReason] = useState(
    "Open incident for correlated operational investigation",
  );
  const mutation = useMutation({
    mutationFn: () =>
      postAPI<"CommandAccepted">(
        "/api/v1/incidents",
        {
          severity,
          reason_code: reasonCode.trim(),
          owner_user_id: owner.trim(),
          expected_revision: "1",
          reason: reason.trim(),
        } satisfies APIModel<"IncidentCreateRequest">,
        newIdempotencyKey("incident-create"),
      ),
    onSuccess: () => client.invalidateQueries({ queryKey: ["incidents"] }),
  });
  return (
    <section className={styles.controlCard} aria-label="Open incident">
      <h2>Open incident</h2>
      <div className={styles.form}>
        <label className={styles.field}>
          Severity
          <select
            value={severity}
            onChange={(event) =>
              setSeverity(event.target.value as typeof severity)
            }
          >
            <option value="warning">Warning</option>
            <option value="error">Error</option>
            <option value="critical">Critical</option>
          </select>
        </label>
        <label className={styles.field}>
          Owner user ID
          <input
            value={owner}
            onChange={(event) => setOwner(event.target.value)}
          />
        </label>
        <label className={styles.field}>
          Stable reason code
          <input
            value={reasonCode}
            onChange={(event) => setReasonCode(event.target.value)}
          />
        </label>
        <label className={styles.field}>
          Operator reason
          <textarea
            minLength={8}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </label>
      </div>
      <button
        className={styles.button}
        type="button"
        disabled={
          mutation.isPending ||
          reason.trim().length < 8 ||
          owner.trim() === "" ||
          reasonCode.trim().length < 3
        }
        onClick={() => mutation.mutate()}
      >
        {mutation.isPending ? "Opening…" : "Open incident"}
      </button>
      {mutation.isError && (
        <p className={styles.error} role="alert">
          Incident creation failed. Verify owner, reason code, permission, and
          revision.
        </p>
      )}
      {mutation.isSuccess && (
        <p className={styles.success} role="status">
          Incident command accepted. Its hash-linked timeline begins when
          applied.
        </p>
      )}
    </section>
  );
}
