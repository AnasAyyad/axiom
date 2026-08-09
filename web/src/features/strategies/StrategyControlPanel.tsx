import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { ownerControlCollectionQuery } from "../../api/queries";
import { ConfirmAction } from "../../components/ConfirmAction";
import { HighRiskAuthorizationForm } from "../shared/HighRiskAuthorizationForm";
import { StatusBadge } from "../shared/StatusBadge";
import { stringAttribute, stringListAttribute } from "./strategyModel";
import styles from "../shared/ConsoleSurface.module.css";

interface StrategyControlPanelProps {
  readonly strategy: APIModel<"OwnerControlResource">;
}

export function StrategyControlPanel({ strategy }: StrategyControlPanelProps) {
  const queryClient = useQueryClient();
  const configurations = useQuery(
    ownerControlCollectionQuery("configuration-revisions"),
  );
  const [configurationID, setConfigurationID] = useState(
    stringAttribute(strategy.attributes, "configuration_id", ""),
  );
  const [configurationState, setConfigurationState] = useState<
    "enabled" | "disabled"
  >("disabled");
  const [runtimeReason, setRuntimeReason] = useState(
    "Operator requested an explicit strategy runtime state change",
  );
  const configured = stringAttribute(
    strategy.attributes,
    "configured_state",
    "disabled",
  );
  const runtime = stringAttribute(
    strategy.attributes,
    "runtime_state",
    strategy.state,
  );
  const blockers = stringListAttribute(
    strategy.attributes,
    "blocking_prerequisites",
  );
  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["strategy"] });
  const runtimeCommand = useMutation({
    mutationFn: (state: "running" | "paused") =>
      postAPI<"CommandAccepted">(
        `/api/v1/strategies/${encodeURIComponent(strategy.id)}/runtime`,
        {
          expected_revision: strategy.revision,
          reason: runtimeReason.trim(),
          state,
        } satisfies APIModel<"RuntimeControlRequest">,
        newIdempotencyKey(`strategy-${state}`),
      ),
    onSuccess: invalidate,
  });
  return (
    <section className={styles.grid} aria-label="Strategy controls">
      <div className={styles.controlCard}>
        <div className={styles.cardHeader}>
          <h2>Configuration and runtime</h2>
          <span>
            <StatusBadge value={configured} /> <StatusBadge value={runtime} />
          </span>
        </div>
        <p>
          On/Off is versioned configuration. Running/Paused/Blocked is runtime
          state. Neither control can enable production-private order submission.
        </p>
        {blockers.length > 0 ? (
          <div className={styles.notice} role="status">
            <strong>Resume is blocked.</strong>
            <ul>
              {blockers.map((blocker) => (
                <li key={blocker}>{blocker.replaceAll("_", " ")}</li>
              ))}
            </ul>
            Resolve every prerequisite and refresh authoritative state before
            retrying.
          </div>
        ) : (
          <p className={styles.success}>
            No blocking prerequisite is reported.
          </p>
        )}
        <>
          <label className={styles.field}>
            Runtime command reason
            <textarea
              value={runtimeReason}
              onChange={(event) => setRuntimeReason(event.target.value)}
              minLength={8}
            />
          </label>
          <div className={styles.actions}>
            <ConfirmAction
              trigger={
                <button
                  className={styles.secondary}
                  type="button"
                  disabled={runtime === "paused" || runtimeCommand.isPending}
                >
                  Pause strategy
                </button>
              }
              title="Pause this strategy?"
              description="New strategy decisions stop after the durable command is applied; audit and safety writes continue."
              confirmLabel="Pause strategy"
              onConfirm={() => runtimeCommand.mutate("paused")}
            />
            <ConfirmAction
              trigger={
                <button
                  className={styles.button}
                  type="button"
                  disabled={
                    runtime === "running" ||
                    configured !== "enabled" ||
                    blockers.length > 0 ||
                    runtimeCommand.isPending
                  }
                >
                  Resume strategy
                </button>
              }
              title="Resume this virtual strategy?"
              description="Resume still fails closed if authoritative readiness changed after this snapshot."
              confirmLabel="Resume strategy"
              onConfirm={() => runtimeCommand.mutate("running")}
            />
          </div>
        </>
        {runtimeCommand.isError && (
          <p className={styles.error} role="alert">
            Runtime command rejected. Refresh the revision and resolve all
            prerequisites before retrying.
          </p>
        )}
      </div>
      <div className={styles.grid}>
        <div className={styles.controlCard}>
          <h2>Versioned configuration</h2>
          <div className={styles.form}>
            <label className={styles.field}>
              Reviewed configuration revision
              <select
                value={configurationID}
                onChange={(event) => setConfigurationID(event.target.value)}
              >
                <option value="">Choose a server-listed revision</option>
                {configurationID !== "" &&
                  !configurations.data?.items.some(
                    (configuration) => configuration.id === configurationID,
                  ) && (
                    <option value={configurationID}>
                      Current recorded revision
                    </option>
                  )}
                {configurations.data?.items.map((configuration) => (
                  <option key={configuration.id} value={configuration.id}>
                    {configuration.state} revision · version{" "}
                    {configuration.revision}
                  </option>
                ))}
              </select>
            </label>
            {configurations.isLoading && <p>Loading reviewed revisions…</p>}
            {configurations.isError && (
              <p className={styles.error} role="alert">
                Reviewed configuration revisions are unavailable. No change can
                be submitted.
              </p>
            )}
            <label className={styles.field}>
              Requested state
              <select
                value={configurationState}
                onChange={(event) =>
                  setConfigurationState(
                    event.target.value as "enabled" | "disabled",
                  )
                }
              >
                <option value="disabled">Off / disabled</option>
                <option value="enabled">On / enabled</option>
              </select>
            </label>
          </div>
        </div>
        <HighRiskAuthorizationForm
          title="Owner reauthentication"
          purpose="strategy_configuration"
          expectedRevision={strategy.revision}
          confirmLabel={`Apply ${configurationState}`}
          disabled={
            configurationID.trim() === "" ||
            configurations.isLoading ||
            configurations.isError
          }
          onAuthorized={async (authorization_token, reason) => {
            await postAPI<"CommandAccepted">(
              `/api/v1/strategies/${encodeURIComponent(strategy.id)}/configuration`,
              {
                authorization_token,
                configuration_id: configurationID.trim(),
                expected_revision: strategy.revision,
                reason,
                state: configurationState,
              } satisfies APIModel<"StrategyConfigurationRequest">,
              newIdempotencyKey("strategy-configuration"),
            );
            await invalidate();
          }}
        />
      </div>
    </section>
  );
}
