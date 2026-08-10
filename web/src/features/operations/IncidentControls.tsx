import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { ConfirmAction } from "../../components/ConfirmAction";
import styles from "../shared/ConsoleSurface.module.css";

export function IncidentControls({
  incident,
}: {
  readonly incident: APIModel<"IncidentDetail">;
}) {
  const client = useQueryClient();
  const [action, setAction] =
    useState<APIModel<"IncidentUpdateRequest">["action"]>("add_remediation");
  const [value, setValue] = useState("");
  const [source, setSource] = useState("qualified decision-input dataset");
  const [first, setFirst] = useState("1");
  const [last, setLast] = useState("1");
  const [reason, setReason] = useState("Update incident after evidence review");
  const [resolution, setResolution] = useState("");
  const update = useMutation({
    mutationFn: () => {
      const body: APIModel<"IncidentUpdateRequest"> = {
        action,
        expected_revision: incident.revision,
        reason: reason.trim(),
      };
      if (action === "assign_owner") body.owner_user_id = value.trim();
      if (action === "add_remediation") body.note = value.trim();
      if (action === "link_alert" || action === "link_activity")
        body.reference_id = value.trim();
      if (action === "link_replay") {
        body.dataset_id = value.trim();
        body.first_ordinal = first;
        body.last_ordinal = last;
        body.source_identity = source.trim();
      }
      return postAPI<"CommandAccepted">(
        `/api/v1/incidents/${encodeURIComponent(incident.id)}/updates`,
        body,
        newIdempotencyKey("incident-update"),
      );
    },
    onSuccess: () =>
      client.invalidateQueries({ queryKey: ["incident", incident.id] }),
  });
  const transition = useMutation({
    mutationFn: (state: "acknowledged" | "resolved") => {
      const body: APIModel<"IncidentTransitionRequest"> = {
        state,
        expected_revision: incident.revision,
        reason: reason.trim(),
      };
      if (state === "resolved") body.resolution_evidence = resolution.trim();
      return postAPI<"CommandAccepted">(
        `/api/v1/incidents/${encodeURIComponent(incident.id)}/transitions`,
        body,
        newIdempotencyKey(`incident-${state}`),
      );
    },
    onSuccess: () =>
      client.invalidateQueries({ queryKey: ["incident", incident.id] }),
  });
  return (
    <section className={styles.controlCard} aria-label="Incident controls">
      <h2>Incident controls</h2>
      <div className={styles.form}>
        <label className={styles.field}>
          Update type
          <select
            value={action}
            onChange={(event) => setAction(event.target.value as typeof action)}
          >
            <option value="add_remediation">Add remediation note</option>
            <option value="assign_owner">Assign owner</option>
            <option value="link_alert">Link alert</option>
            <option value="link_activity">Link activity</option>
            <option value="link_replay">Link replay input</option>
          </select>
        </label>
        <label className={styles.field}>
          {action === "add_remediation"
            ? "Remediation note"
            : action === "link_replay"
              ? "Dataset ID"
              : "Reference or owner ID"}
          <textarea
            value={value}
            onChange={(event) => setValue(event.target.value)}
          />
        </label>
        {action === "link_replay" && (
          <>
            <label className={styles.field}>
              First ordinal
              <input
                value={first}
                onChange={(event) => setFirst(event.target.value)}
              />
            </label>
            <label className={styles.field}>
              Last ordinal
              <input
                value={last}
                onChange={(event) => setLast(event.target.value)}
              />
            </label>
            <label className={styles.field}>
              Source identity
              <input
                value={source}
                onChange={(event) => setSource(event.target.value)}
              />
            </label>
          </>
        )}
        <label className={styles.field}>
          Audited reason
          <textarea
            minLength={8}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </label>
        <label className={styles.field}>
          Resolution evidence
          <textarea
            placeholder="Required only when resolving"
            value={resolution}
            onChange={(event) => setResolution(event.target.value)}
          />
        </label>
      </div>
      <div className={styles.actions}>
        <button
          className={styles.secondary}
          type="button"
          disabled={
            update.isPending ||
            value.trim().length < 1 ||
            reason.trim().length < 8
          }
          onClick={() => update.mutate()}
        >
          Apply update
        </button>
        {incident.state === "open" && (
          <ConfirmAction
            trigger={
              <button className={styles.button} type="button">
                Acknowledge
              </button>
            }
            title="Acknowledge this incident?"
            description="This records ownership and exact revision without resolving the cause."
            confirmLabel="Acknowledge incident"
            onConfirm={() => transition.mutate("acknowledged")}
          />
        )}
        <ConfirmAction
          trigger={
            <button
              className={styles.danger}
              type="button"
              disabled={resolution.trim().length < 3}
            >
              Resolve
            </button>
          }
          title="Resolve with evidence?"
          description="Resolution requires durable evidence and closes this lifecycle revision."
          confirmLabel="Resolve incident"
          onConfirm={() => transition.mutate("resolved")}
        />
      </div>
      {(update.isError || transition.isError) && (
        <p className={styles.error} role="alert">
          Incident command rejected. Refresh the exact revision and verify all
          required evidence.
        </p>
      )}
    </section>
  );
}
