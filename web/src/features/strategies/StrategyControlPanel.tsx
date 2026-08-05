import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { ConfirmAction } from "../../components/ConfirmAction";
import { hasAccess } from "../shared/access";
import { HighRiskAuthorizationForm } from "../shared/HighRiskAuthorizationForm";
import { StatusBadge } from "../shared/StatusBadge";
import { stringAttribute, stringListAttribute } from "./strategyModel";
import styles from "../shared/ConsoleSurface.module.css";

interface StrategyControlPanelProps {
  readonly strategy: APIModel<"D1Resource">;
  readonly user: APIModel<"SessionUser">;
}

export function StrategyControlPanel({
  strategy,
  user,
}: StrategyControlPanelProps) {
  const queryClient = useQueryClient();
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
  const canConfigure = hasAccess(user, ["configuration.admin"]);
  const canControl = hasAccess(user, ["operations.control"]);
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
        {canControl ? (
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
        ) : (
          <p className={styles.heroNote}>
            Your role may review this state but cannot change runtime strategy
            controls.
          </p>
        )}
        {runtimeCommand.isError && (
          <p className={styles.error} role="alert">
            Runtime command rejected. Refresh the revision and resolve all
            prerequisites before retrying.
          </p>
        )}
      </div>
      {canConfigure && (
        <div className={styles.grid}>
          <div className={styles.controlCard}>
            <h2>Versioned configuration</h2>
            <div className={styles.form}>
              <label className={styles.field}>
                Configuration ID
                <input
                  value={configurationID}
                  onChange={(event) => setConfigurationID(event.target.value)}
                  required
                />
              </label>
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
            disabled={configurationID.trim() === ""}
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
      )}
    </section>
  );
}
