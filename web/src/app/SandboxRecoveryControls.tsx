import type { APIModel } from "../api/client";
import { ConfirmAction } from "../components/ConfirmAction";
import styles from "./SandboxControls.module.css";
import type { SandboxLowRiskAction } from "./SandboxControlsView";

interface Props {
  readonly account: APIModel<"SandboxAccount"> | undefined;
  readonly accountID: string;
  readonly orders: APIModel<"SandboxOrder">[];
  readonly canArm: boolean;
  readonly canCancel: boolean;
  readonly canAdmin: boolean;
  readonly onAction: (action: SandboxLowRiskAction) => void;
}

export function SandboxRecoveryControls(props: Props) {
  return (
    <div className={styles.actions} aria-label="Recovery actions">
      {props.account?.active_arm && props.canArm && (
        <ConfirmAction
          trigger={<button type="button">Revoke active arm</button>}
          title="Revoke this test/demo arm?"
          description="New entries stop immediately. Safe cancellation, query, and reconciliation remain available."
          confirmLabel="Revoke arm"
          onConfirm={() =>
            props.onAction({
              kind: "revoke",
              arm: props.account!.active_arm!,
            })
          }
        />
      )}
      {props.account && props.canAdmin && (
        <ConfirmAction
          trigger={<button type="button">Reconcile account</button>}
          title="Queue authoritative account reconciliation?"
          description="The credential-owning engine will perform the private read. No browser or API credential is used."
          confirmLabel="Queue reconciliation"
          onConfirm={() =>
            props.onAction({ kind: "reconcile", account: props.account! })
          }
        />
      )}
      {props.orders
        .filter(
          (item) =>
            item.account_id === props.accountID &&
            !["FILLED", "CANCELED", "REJECTED", "EXPIRED"].includes(item.state),
        )
        .map((item) => (
          <span className={styles.orderActions} key={item.id}>
            {props.canCancel && (
              <ConfirmAction
                trigger={<button type="button">Cancel {item.id}</button>}
                title="Queue safe test/demo cancellation?"
                description="Cancellation remains available while entry is paused or locked."
                confirmLabel="Queue cancellation"
                onConfirm={() =>
                  props.onAction({ kind: "cancel", order: item })
                }
              />
            )}
            {props.canCancel && (
              <button
                type="button"
                onClick={() => props.onAction({ kind: "query", order: item })}
              >
                Query {item.id}
              </button>
            )}
          </span>
        ))}
    </div>
  );
}
