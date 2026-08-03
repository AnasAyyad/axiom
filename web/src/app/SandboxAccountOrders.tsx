import { useEffect, useState } from "react";

import type { APIModel } from "../api/client";
import { StatePanel } from "../components/StatePanel";
import { SandboxFact } from "./SandboxFact";
import styles from "./SandboxOperationsPage.module.css";
import { sandboxEnvironmentName, yesNo } from "./sandboxPresentation";

export function SandboxAccountGrid({
  accounts,
}: {
  readonly accounts: APIModel<"SandboxAccount">[];
}) {
  const now = useSecondClock();
  return (
    <section aria-labelledby="sandbox-accounts-heading">
      <h2 id="sandbox-accounts-heading">Accounts and entry capacity</h2>
      <div className={styles.grid}>
        {accounts.map((account) => {
          const arm = account.active_arm;
          return (
            <article className={styles.card} key={account.id}>
              <header>
                <div>
                  <span>{sandboxEnvironmentName(account)}</span>
                  <h3>{account.id}</h3>
                </div>
                <strong data-state={account.engine_ready ? "good" : "warn"}>
                  {account.state}
                </strong>
              </header>
              <dl>
                <SandboxFact
                  label="Engine ready"
                  value={yesNo(account.engine_ready)}
                />
                <SandboxFact
                  label="Private stream"
                  value={yesNo(account.private_stream_healthy)}
                />
                <SandboxFact
                  label="Reconciliation"
                  value={account.reconciliation_clean ? "clean" : "blocked"}
                />
                <SandboxFact
                  label="Lease held"
                  value={yesNo(account.lease_held)}
                />
                <SandboxFact
                  label="Account epoch"
                  value={String(account.account_epoch)}
                />
                <SandboxFact
                  label="Arm"
                  value={
                    arm
                      ? `${arm.state} · ${countdown(arm.expires_at, now)}`
                      : "not armed"
                  }
                />
                <SandboxFact
                  label="Per-order cap"
                  value={`${account.cap_usage.per_order_limit} USDT`}
                />
                <SandboxFact
                  label="Daily remaining"
                  value={`${account.cap_usage.daily_remaining} / ${account.cap_usage.daily_limit} USDT`}
                />
                <SandboxFact
                  label="Open capacity"
                  value={`${account.cap_usage.account_open}/${account.cap_usage.account_open_limit} account · ${account.cap_usage.global_open}/${account.cap_usage.global_open_limit} global`}
                />
              </dl>
              <a href={account.audit_url}>Open account audit evidence</a>
            </article>
          );
        })}
      </div>
    </section>
  );
}

export function SandboxOrderLedger({
  orders,
}: {
  readonly orders: APIModel<"SandboxOrder">[];
}) {
  return (
    <section aria-labelledby="sandbox-orders-heading">
      <h2 id="sandbox-orders-heading">Orders, attempts, fills, and recovery</h2>
      {orders.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No test or demo orders have been durably approved."
        />
      ) : (
        <div className={styles.table}>
          <table>
            <caption>Test/demo order lifecycle</caption>
            <thead>
              <tr>
                <th>Venue</th>
                <th>Order</th>
                <th>State</th>
                <th>Exact request</th>
                <th>Attempts</th>
                <th>Fills</th>
                <th>Recovery</th>
                <th>Audit</th>
              </tr>
            </thead>
            <tbody>
              {orders.map((order) => (
                <tr key={order.id}>
                  <td>{sandboxEnvironmentName(order)}</td>
                  <td>{order.id}</td>
                  <td data-state={order.state === "UNKNOWN" ? "warn" : "good"}>
                    {order.state}
                  </td>
                  <td>
                    {order.side} {order.quantity} {order.instrument} @{" "}
                    {order.limit_price} · {order.style}
                  </td>
                  <td>{order.attempt}</td>
                  <td>
                    {order.fills.length === 0
                      ? "none"
                      : order.fills
                          .map(
                            (fill) =>
                              `${fill.quantity} @ ${fill.price}; fee ${fill.fee_quantity} ${fill.fee_asset}`,
                          )
                          .join(" · ")}
                  </td>
                  <td>{order.recovery_status}</td>
                  <td>
                    <a href={order.audit_url}>Evidence</a>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}

function useSecondClock() {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1_000);
    return () => window.clearInterval(timer);
  }, []);
  return now;
}

function countdown(expiresAt: string, now: number) {
  const seconds = Math.max(0, Math.ceil((Date.parse(expiresAt) - now) / 1_000));
  const minutes = Math.floor(seconds / 60);
  return `${String(minutes).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
}
