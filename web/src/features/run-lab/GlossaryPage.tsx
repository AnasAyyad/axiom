import { Page } from "../../app/OperationalShared";
import styles from "../shared/ConsoleSurface.module.css";

const glossary = [
  [
    "Shadow trading",
    "A public-data-only simulation that never sends an exchange order.",
  ],
  ["Backtest", "A deterministic run against approved historical inputs."],
  [
    "Replay",
    "A deterministic re-run of recorded market input in its original order.",
  ],
  [
    "Testnet",
    "Binance's isolated spot test environment; it is not real-money trading.",
  ],
  [
    "Demo",
    "Bybit's isolated Demo account boundary; it is not real-money trading.",
  ],
  [
    "Arm",
    "A short, owner-authorized period that permits capped sandbox entries after checks pass.",
  ],
  [
    "Reconciliation",
    "Comparing the authoritative account state with Axiom's durable records.",
  ],
  [
    "Stale data",
    "Required public input is older than its safety limit, so new decisions are paused.",
  ],
  [
    "Drawdown",
    "The decline from a prior portfolio high; it is a risk measure, not a prediction.",
  ],
  [
    "Realized P&L",
    "Gain or loss recorded after a position is reduced or closed.",
  ],
  [
    "Unrealized P&L",
    "The current mark-to-market change of an open virtual or sandbox position.",
  ],
  [
    "Slippage",
    "The difference between a planned price and simulated or acknowledged execution.",
  ],
  [
    "Reservation",
    "A temporary ownership claim that prevents the same balance or liquidity being used twice.",
  ],
  [
    "Risk lock",
    "A fail-closed risk state that blocks new entries until its recovery conditions are met.",
  ],
  [
    "Confidence tier",
    "How strong and complete the supporting input and evidence are; it is not profitability.",
  ],
  [
    "Qualification",
    "A separately governed verification process. Passing a product screen does not certify profitability.",
  ],
] as const;

export function GlossaryPage() {
  return (
    <Page
      title="Glossary"
      eyebrow="Plain-English product terms"
      description="Definitions used throughout the owner console. Technical identifiers remain in advanced evidence details."
    >
      <section className={styles.notice}>
        <h2>How to use these terms</h2>
        <p>
          These definitions explain what a value or workflow means and what it
          does not prove. They do not relax any safety limit.
        </p>
      </section>
      <dl className={styles.facts}>
        {glossary.map(([term, definition]) => (
          <div key={term}>
            <dt>{term}</dt>
            <dd>{definition}</dd>
          </div>
        ))}
      </dl>
    </Page>
  );
}
