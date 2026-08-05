import type { Dispatch, SetStateAction } from "react";

import type { APIModel } from "../api/client";
import styles from "./SandboxControls.module.css";
import { SandboxHighRiskControl } from "./SandboxHighRiskControl";
import { SandboxMutationResult } from "./SandboxMutationResult";
import { SandboxOrderEntryControl } from "./SandboxOrderEntryControl";
import { SandboxRecoveryControls } from "./SandboxRecoveryControls";

type Account = APIModel<"SandboxAccount">;
type Order = APIModel<"SandboxOrder">;
type HighRiskAction = "arm" | "unlock";
type Instrument = "BTCUSDT" | "ETHUSDT";
type OrderStyle = "LIMIT_GTC" | "LIMIT_IOC" | "POST_ONLY";

export type SandboxLowRiskAction =
  | { kind: "revoke"; arm: APIModel<"SandboxArm"> }
  | { kind: "cancel" | "query"; order: Order }
  | { kind: "reconcile"; account: Account };

interface Props {
  readonly accounts: Account[];
  readonly orders: Order[];
  readonly account: Account | undefined;
  readonly cleanReconciliation: APIModel<"SandboxReconciliation"> | undefined;
  readonly accountID: string;
  readonly setAccountID: Dispatch<SetStateAction<string>>;
  readonly lowRiskReason: string;
  readonly setLowRiskReason: Dispatch<SetStateAction<string>>;
  readonly highRiskAction: HighRiskAction;
  readonly setHighRiskAction: Dispatch<SetStateAction<HighRiskAction>>;
  readonly password: string;
  readonly setPassword: Dispatch<SetStateAction<string>>;
  readonly totp: string;
  readonly setTOTP: Dispatch<SetStateAction<string>>;
  readonly highRiskReason: string;
  readonly setHighRiskReason: Dispatch<SetStateAction<string>>;
  readonly confirmed: boolean;
  readonly setConfirmed: Dispatch<SetStateAction<boolean>>;
  readonly instrument: Instrument;
  readonly setInstrument: Dispatch<SetStateAction<Instrument>>;
  readonly style: OrderStyle;
  readonly setStyle: Dispatch<SetStateAction<OrderStyle>>;
  readonly quantity: string;
  readonly setQuantity: Dispatch<SetStateAction<string>>;
  readonly limitPrice: string;
  readonly setLimitPrice: Dispatch<SetStateAction<string>>;
  readonly orderConfirmed: boolean;
  readonly setOrderConfirmed: Dispatch<SetStateAction<boolean>>;
  readonly highRiskPending: boolean;
  readonly orderPending: boolean;
  readonly onHighRisk: () => void;
  readonly onOrder: () => void;
  readonly onLowRisk: (action: SandboxLowRiskAction) => void;
  readonly errors: unknown[];
  readonly pending: boolean;
  readonly success: boolean;
}

export function SandboxControlsView(props: Props) {
  const canArm = true;
  const canCancel = true;
  const canAdmin = true;
  return (
    <section className={styles.controls} aria-labelledby="controls-heading">
      <header>
        <div>
          <span>Audited operator controls</span>
          <h2 id="controls-heading">Virtual/test actions only</h2>
        </div>
        <strong>SERVER POLICY IS AUTHORITATIVE</strong>
      </header>
      <p>
        No production target exists. Disabled controls are only guidance; the
        server independently rechecks session, Origin, CSRF,
        idempotency, revision, reconciliation, arm, caps, and configuration.
      </p>
      <label>
        Account
        <select
          value={props.accountID}
          onChange={(event) => props.setAccountID(event.target.value)}
        >
          {props.accounts.map((item) => (
            <option key={item.id} value={item.id}>
              {item.exchange === "binance"
                ? "Binance Spot Testnet"
                : "Bybit Demo"}{" "}
              · {item.id}
            </option>
          ))}
        </select>
      </label>
      <label>
        Durable command reason
        <input
          value={props.lowRiskReason}
          minLength={8}
          maxLength={500}
          onChange={(event) => props.setLowRiskReason(event.target.value)}
        />
      </label>
      <div className={styles.grid}>
        {(canArm || canAdmin) && (
          <SandboxHighRiskControl
            {...props}
            canArm={canArm}
            canAdmin={canAdmin}
          />
        )}
        {canArm && <SandboxOrderEntryControl {...props} />}
      </div>
      <SandboxRecoveryControls
        account={props.account}
        accountID={props.accountID}
        orders={props.orders}
        canArm={canArm}
        canCancel={canCancel}
        canAdmin={canAdmin}
        onAction={props.onLowRisk}
      />
      <SandboxMutationResult
        errors={props.errors}
        pending={props.pending}
        success={props.success}
      />
    </section>
  );
}
