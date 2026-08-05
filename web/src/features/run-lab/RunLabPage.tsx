import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";

import { runCatalogQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import styles from "../shared/D2.module.css";

const destinations: Record<string, string> = {
  backtest: "/backtests",
  replay: "/replays",
  shadow: "/shadow",
  demonstration: "/guided-demonstrations",
  testnet: "/operations/sandbox",
  demo: "/operations/sandbox",
};

function modeLabel(mode: string) {
  return mode === "testnet"
    ? "Binance Spot Testnet"
    : mode === "demo"
      ? "Bybit Demo"
      : mode;
}

export function RunLabPage() {
  const catalog = useQuery(runCatalogQuery);
  if (catalog.isLoading) return <StatePanel state="loading" />;
  if (catalog.isError || !catalog.data)
    return (
      <StatePanel
        state="error"
        detail="Approved run choices are unavailable."
      />
    );
  return (
    <Page
      title="New Run"
      eyebrow="Choose a reviewed workflow"
      description="Every choice comes from the server. You never need to copy a dataset, configuration, portfolio, or model ID."
    >
      <details className={styles.notice}>
        <summary>About this page</summary>
        <p>
          A run follows the shared strategy, allocation, risk, execution,
          accounting, and reconciliation path. Historical, replay, shadow,
          Testnet, and Demo outcomes are research or integration evidence; they
          do not prove profitability.
        </p>
      </details>
      {catalog.data.blocker && (
        <StatePanel
          state="blocked"
          detail={`${catalog.data.blocker.summary} ${catalog.data.blocker.suggested_action}`}
        />
      )}
      <div className={styles.cardGrid}>
        {catalog.data.choices.map((choice) => {
          const destination = destinations[choice.mode];
          return (
            <article
              className={styles.card}
              key={`${choice.strategy_id}-${choice.mode}-${choice.instrument}-${choice.exchanges.join("-")}`}
            >
              <div className={styles.cardHeader}>
                <h2>{choice.strategy_name}</h2>
                <span>{modeLabel(choice.mode)}</span>
              </div>
              <p>
                {choice.instrument} · {choice.exchanges.join(" and ")}
              </p>
              <dl className={styles.facts}>
                <div>
                  <dt>When it evaluates</dt>
                  <dd>{choice.cadence}</dd>
                </div>
                <div>
                  <dt>Before it can start</dt>
                  <dd>{choice.warmup}</dd>
                </div>
                <div>
                  <dt>What it can do</dt>
                  <dd>
                    {choice.order_capable
                      ? "Uses the shared simulated or sandbox order pipeline."
                      : "Advisory recommendations only; it never submits a transfer or order."}
                  </dd>
                </div>
              </dl>
              {destination ? (
                <Link className={styles.linkButton} to={destination}>
                  Continue with this run
                </Link>
              ) : (
                <p className={styles.notice}>
                  This reviewed workflow will be available when its guided
                  scenario is installed.
                </p>
              )}
            </article>
          );
        })}
      </div>
    </Page>
  );
}
