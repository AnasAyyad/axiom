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
import { HighRiskAuthorizationForm } from "../shared/HighRiskAuthorizationForm";
import { StatusBadge } from "../shared/StatusBadge";
import { stringAttribute } from "../strategies/strategyModel";
import styles from "../shared/D2.module.css";

export function RiskControlsPage() {
  const session = useQuery(sessionQuery);
  const query = useQuery(d1CollectionQuery("risk/controls"));
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
        detail="Scoped risk controls are unavailable."
      />
    );
  const canControl = hasAccess(session.data.user, ["operations.control"]);
  const isOwner = session.data.user.roles.includes("owner");
  return (
    <Page
      title="Scoped Risk Controls"
      eyebrow="Global and bounded fail-closed state"
      description="Pause or lock specific strategies, instruments, exchanges, or new entries. Returning to normal requires Owner reauthentication and clean readiness."
    >
      <p className={styles.notice} role="note">
        Risk controls cannot enable leverage, short selling, unowned-asset
        sales, production-private submission, or any other prohibited V1
        capability.
      </p>
      {query.data.items.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No scoped risk controls have been recorded."
        />
      ) : (
        <div className={styles.cardGrid}>
          {query.data.items.map((control) => (
            <RiskControlCard
              key={control.id}
              control={control}
              canControl={canControl}
              isOwner={isOwner}
            />
          ))}
        </div>
      )}
    </Page>
  );
}

function RiskControlCard({
  control,
  canControl,
  isOwner,
}: {
  readonly control: APIModel<"D1Resource">;
  readonly canControl: boolean;
  readonly isOwner: boolean;
}) {
  const queryClient = useQueryClient();
  const [reason, setReason] = useState(
    "Operator requested a scoped fail-closed risk state change",
  );
  const scope = stringAttribute(control.attributes, "scope", "global");
  const scopeID = stringAttribute(control.attributes, "scope_id", "all");
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ["d1", "risk/controls"] });
  const mutation = useMutation({
    mutationFn: (state: "paused" | "locked") =>
      postAPI<"CommandAccepted">(
        `/api/v1/risk/controls/${encodeURIComponent(scope)}/${encodeURIComponent(scopeID)}`,
        {
          expected_revision: control.revision,
          reason: reason.trim(),
          state,
        } satisfies APIModel<"RiskControlRequest">,
        newIdempotencyKey(`risk-${state}`),
      ),
    onSuccess: refresh,
  });
  return (
    <article className={styles.card}>
      <div className={styles.cardHeader}>
        <div>
          <h2>{scope.replaceAll("_", " ")}</h2>
          <p>{scopeID}</p>
        </div>
        <StatusBadge value={control.state} />
      </div>
      <dl className={styles.facts}>
        <div>
          <dt>Reason code</dt>
          <dd>{stringAttribute(control.attributes, "reason_code")}</dd>
        </div>
        <div>
          <dt>Revision</dt>
          <dd>{control.revision}</dd>
        </div>
      </dl>
      {canControl ? (
        <>
          <label className={styles.field}>
            Control reason
            <textarea
              value={reason}
              onChange={(event) => setReason(event.target.value)}
            />
          </label>
          <div className={styles.actions}>
            <ConfirmAction
              trigger={
                <button className={styles.secondary} type="button">
                  Pause scope
                </button>
              }
              title="Pause this risk scope?"
              description="New affected activity stops after the durable command applies."
              confirmLabel="Pause scope"
              onConfirm={() => mutation.mutate("paused")}
            />
            <ConfirmAction
              trigger={
                <button className={styles.danger} type="button">
                  Lock scope
                </button>
              }
              title="Lock this risk scope?"
              description="The lock remains fail-closed until an Owner reauthenticates and readiness passes."
              confirmLabel="Lock scope"
              onConfirm={() => mutation.mutate("locked")}
            />
          </div>
          {isOwner && (
            <HighRiskAuthorizationForm
              title="Return scope to normal"
              purpose="risk_control"
              expectedRevision={control.revision}
              confirmLabel="Request normal state"
              onAuthorized={async (authorization_token, authorizedReason) => {
                await postAPI<"CommandAccepted">(
                  `/api/v1/risk/controls/${encodeURIComponent(scope)}/${encodeURIComponent(scopeID)}`,
                  {
                    authorization_token,
                    expected_revision: control.revision,
                    reason: authorizedReason,
                    state: "normal",
                  } satisfies APIModel<"RiskControlRequest">,
                  newIdempotencyKey("risk-normal"),
                );
                await refresh();
              }}
            />
          )}
          {mutation.isError && (
            <p className={styles.error} role="alert">
              The scoped command was rejected. Refresh the revision and retry.
            </p>
          )}
        </>
      ) : (
        <p className={styles.heroNote}>
          Your role can inspect this risk state but cannot mutate it.
        </p>
      )}
    </article>
  );
}
