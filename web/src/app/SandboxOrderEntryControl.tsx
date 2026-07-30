import type { FormEvent } from "react";

import type { APIModel } from "../api/client";
import styles from "./SandboxControls.module.css";

interface Props {
  readonly account: APIModel<"SandboxAccount"> | undefined;
  readonly instrument: "BTCUSDT" | "ETHUSDT";
  readonly setInstrument: (value: "BTCUSDT" | "ETHUSDT") => void;
  readonly style: "LIMIT_GTC" | "LIMIT_IOC" | "POST_ONLY";
  readonly setStyle: (value: "LIMIT_GTC" | "LIMIT_IOC" | "POST_ONLY") => void;
  readonly quantity: string;
  readonly setQuantity: (value: string) => void;
  readonly limitPrice: string;
  readonly setLimitPrice: (value: string) => void;
  readonly orderConfirmed: boolean;
  readonly setOrderConfirmed: (value: boolean) => void;
  readonly orderPending: boolean;
  readonly onOrder: () => void;
}

export function SandboxOrderEntryControl(props: Props) {
  return (
    <form
      className={styles.card}
      onSubmit={(event: FormEvent) => {
        event.preventDefault();
        props.onOrder();
      }}
    >
      <h3>Buy-only test order</h3>
      <label>
        Instrument
        <select
          value={props.instrument}
          onChange={(event) =>
            props.setInstrument(event.target.value as "BTCUSDT" | "ETHUSDT")
          }
        >
          <option value="BTCUSDT">BTCUSDT</option>
          <option value="ETHUSDT">ETHUSDT</option>
        </select>
      </label>
      <label>
        Style
        <select
          value={props.style}
          onChange={(event) =>
            props.setStyle(
              event.target.value as "LIMIT_GTC" | "LIMIT_IOC" | "POST_ONLY",
            )
          }
        >
          <option value="LIMIT_GTC">LIMIT_GTC</option>
          <option value="LIMIT_IOC">LIMIT_IOC</option>
          <option value="POST_ONLY">POST_ONLY</option>
        </select>
      </label>
      <label>
        Exact quantity
        <input
          inputMode="decimal"
          value={props.quantity}
          onChange={(event) => props.setQuantity(event.target.value)}
        />
      </label>
      <label>
        Exact limit price
        <input
          inputMode="decimal"
          value={props.limitPrice}
          onChange={(event) => props.setLimitPrice(event.target.value)}
        />
      </label>
      <label className={styles.confirm}>
        <input
          type="checkbox"
          checked={props.orderConfirmed}
          onChange={(event) => props.setOrderConfirmed(event.target.checked)}
        />
        I confirm this is a capped virtual BUY, never a production order.
      </label>
      <button
        type="submit"
        disabled={
          props.orderPending ||
          !props.orderConfirmed ||
          props.quantity === "" ||
          props.limitPrice === "" ||
          props.account?.active_arm?.state !== "active"
        }
      >
        Request capped test order
      </button>
    </form>
  );
}
