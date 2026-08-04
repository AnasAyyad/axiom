import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { alertRoutesQuery } from "../../api/queries";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import styles from "../shared/D2.module.css";

export function AlertRoutePanel({ canTest }: { readonly canTest: boolean }) {
  const query = useQuery(alertRoutesQuery);
  if (query.isLoading) return <StatePanel state="loading" />;
  if (query.isError || !query.data)
    return (
      <StatePanel
        state="error"
        detail="Sanitized alert routes are unavailable."
      />
    );
  return (
    <section aria-labelledby="alert-routes-title">
      <h2 id="alert-routes-title">Delivery routes</h2>
      <p>
        Only route labels and delivery outcomes are shown. Endpoint URLs,
        credentials, headers, and private payloads are never exposed.
      </p>
      <div className={styles.cardGrid}>
        {query.data.items.map((route) => (
          <AlertRouteCard key={route.id} route={route} canTest={canTest} />
        ))}
      </div>
    </section>
  );
}

function AlertRouteCard({
  route,
  canTest,
}: {
  readonly route: APIModel<"AlertRoute">;
  readonly canTest: boolean;
}) {
  const client = useQueryClient();
  const [reason, setReason] = useState(
    "Send a sanitized test delivery and record the outcome",
  );
  const mutation = useMutation({
    mutationFn: () =>
      postAPI<"CommandAccepted">(
        `/api/v1/alert-routes/${encodeURIComponent(route.id)}/test`,
        {
          expected_revision: route.revision,
          reason: reason.trim(),
        } satisfies APIModel<"AlertTestRequest">,
        newIdempotencyKey("alert-route-test"),
      ),
    onSuccess: () => client.invalidateQueries({ queryKey: ["alert-routes"] }),
  });
  return (
    <article className={styles.card}>
      <div className={styles.cardHeader}>
        <h3>{route.sink_name.replaceAll("_", " ")}</h3>
        <StatusBadge value={route.enabled ? "enabled" : "disabled"} />
      </div>
      <dl className={styles.facts}>
        <div>
          <dt>Minimum severity</dt>
          <dd>{route.minimum_severity}</dd>
        </div>
        <div>
          <dt>Target</dt>
          <dd>{route.target_label ?? "In application"}</dd>
        </div>
        <div>
          <dt>Last test</dt>
          <dd>{route.last_test_state ?? "Not tested"}</dd>
        </div>
        <div>
          <dt>Revision</dt>
          <dd>{route.revision}</dd>
        </div>
      </dl>
      {canTest && route.enabled && (
        <div className={styles.controlCard}>
          <label className={styles.field}>
            Test reason
            <textarea
              minLength={8}
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </label>
          <button
            className={styles.secondary}
            type="button"
            disabled={mutation.isPending || reason.trim().length < 8}
            onClick={() => mutation.mutate()}
          >
            {mutation.isPending ? "Sending…" : "Send sanitized test"}
          </button>
          {mutation.isError && (
            <p className={styles.error} role="alert">
              Test delivery was rejected. Check route state, permission, and
              revision.
            </p>
          )}
          {mutation.isSuccess && (
            <p className={styles.success} role="status">
              Test command accepted. Delivery attempts and failures remain
              durable.
            </p>
          )}
        </div>
      )}
      <EvidenceDetails
        summary="Allowlisted route state; secret transport configuration is excluded."
        value={route}
      />
    </article>
  );
}
