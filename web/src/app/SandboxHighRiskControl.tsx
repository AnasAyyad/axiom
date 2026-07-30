import type { FormEvent } from "react";

import type { APIModel } from "../api/client";
import styles from "./SandboxControls.module.css";

interface Props {
  readonly account: APIModel<"SandboxAccount"> | undefined;
  readonly cleanReconciliation: APIModel<"SandboxReconciliation"> | undefined;
  readonly highRiskAction: "arm" | "unlock";
  readonly setHighRiskAction: (value: "arm" | "unlock") => void;
  readonly password: string;
  readonly setPassword: (value: string) => void;
  readonly totp: string;
  readonly setTOTP: (value: string) => void;
  readonly highRiskReason: string;
  readonly setHighRiskReason: (value: string) => void;
  readonly confirmed: boolean;
  readonly setConfirmed: (value: boolean) => void;
  readonly highRiskPending: boolean;
  readonly canArm: boolean;
  readonly canAdmin: boolean;
  readonly onHighRisk: () => void;
}

export function SandboxHighRiskControl(props: Props) {
  return (
    <form
      className={styles.card}
      onSubmit={(event: FormEvent) => {
        event.preventDefault();
        props.onHighRisk();
      }}
    >
      <h3>High-risk authorization</h3>
      <label>
        Action
        <select
          value={props.highRiskAction}
          onChange={(event) =>
            props.setHighRiskAction(event.target.value as "arm" | "unlock")
          }
        >
          {props.canArm && <option value="arm">Create 15-minute arm</option>}
          {props.canAdmin && <option value="unlock">Risk unlock</option>}
        </select>
      </label>
      <label>
        Password
        <input
          type="password"
          autoComplete="current-password"
          value={props.password}
          onChange={(event) => props.setPassword(event.target.value)}
        />
      </label>
      <label>
        Six-digit TOTP
        <input
          inputMode="numeric"
          autoComplete="one-time-code"
          pattern="[0-9]{6}"
          value={props.totp}
          onChange={(event) => props.setTOTP(event.target.value)}
        />
      </label>
      <label>
        High-risk reason
        <input
          minLength={8}
          maxLength={500}
          value={props.highRiskReason}
          onChange={(event) => props.setHighRiskReason(event.target.value)}
        />
      </label>
      <label className={styles.confirm}>
        <input
          type="checkbox"
          checked={props.confirmed}
          onChange={(event) => props.setConfirmed(event.target.checked)}
        />
        I confirm this targets only Binance Spot Testnet or Bybit Demo.
      </label>
      <button
        type="submit"
        disabled={
          props.highRiskPending ||
          !props.confirmed ||
          props.password === "" ||
          !/^[0-9]{6}$/.test(props.totp) ||
          (props.highRiskAction === "arm" &&
            (!props.account?.engine_ready ||
              !props.account.session_id ||
              props.account.active_arm !== undefined)) ||
          (props.highRiskAction === "unlock" &&
            (!props.cleanReconciliation ||
              (props.account?.state !== "LOCKED" &&
                props.account?.state !== "QUARANTINED")))
        }
      >
        {props.highRiskAction === "arm"
          ? "Authorize and arm virtual session"
          : "Authorize reconciled unlock"}
      </button>
      <small>
        Password, TOTP, and the one-use grant remain only in request memory and
        are cleared immediately.
      </small>
    </form>
  );
}
