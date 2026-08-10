import { Link } from "react-router";
import type { APIModel } from "../../api/client";
import styles from "../shared/ConsoleSurface.module.css";

type RunMode = APIModel<"RunChoice">["mode"];

interface RunChoiceWizardProps {
  readonly catalog: APIModel<"RunCatalog">;
  readonly purpose: RunMode | "";
  readonly strategyID: string;
  readonly selectedChoiceKey: string;
  readonly createPending: boolean;
  readonly onPurpose: (mode: RunMode) => void;
  readonly onStrategy: (id: string) => void;
  readonly onChoice: (key: string) => void;
  readonly onCreate: (choice: APIModel<"RunChoice">) => void;
}

const purposes: ReadonlyArray<{
  readonly mode: RunMode;
  readonly label: string;
  readonly description: string;
}> = [
  {
    mode: "demonstration",
    label: "Guided proof",
    description:
      "A synthetic, deterministic walkthrough of the shared pipeline.",
  },
  {
    mode: "backtest",
    label: "Historical test",
    description: "A durable run over qualified recorded inputs.",
  },
  {
    mode: "replay",
    label: "Recorded replay",
    description: "A deterministic event-by-event reproduction.",
  },
  {
    mode: "shadow",
    label: "Live public-data shadow",
    description: "Public market data with virtual execution only.",
  },
  {
    mode: "sandbox",
    label: "Exchange sandbox",
    description:
      "An explicitly armed Binance Spot Testnet, Bybit Demo, or paired strategy session.",
  },
];

function choiceKey(choice: APIModel<"RunChoice">) {
  return [
    choice.strategy_id,
    choice.mode,
    choice.instrument,
    ...choice.exchanges,
  ].join(":");
}

export function RunChoiceWizard({
  catalog,
  purpose,
  strategyID,
  selectedChoiceKey,
  createPending,
  onPurpose,
  onStrategy,
  onChoice,
  onCreate,
}: RunChoiceWizardProps) {
  const purposeChoices = catalog.choices.filter(
    (choice) => choice.mode === purpose,
  );
  const strategies = purposeChoices.filter(
    (choice, index, choices) =>
      choices.findIndex(
        (candidate) => candidate.strategy_id === choice.strategy_id,
      ) === index,
  );
  const compatibleChoices = purposeChoices.filter(
    (choice) => choice.strategy_id === strategyID,
  );
  const selectedChoice = catalog.choices.find(
    (choice) => choiceKey(choice) === selectedChoiceKey,
  );
  return (
    <>
      <section className={styles.section} aria-labelledby="run-purpose">
        <h2 id="run-purpose">1. Choose a purpose</h2>
        <p>Only installed, compatible workflows can move to the next step.</p>
        <div className={styles.cardGrid}>
          {purposes.map((item) => {
            const available =
              item.mode === "demonstration" ||
              catalog.choices.some((choice) => choice.mode === item.mode);
            return (
              <button
                aria-pressed={purpose === item.mode}
                className={styles.card}
                disabled={!available}
                key={item.mode}
                onClick={() => onPurpose(item.mode)}
                type="button"
              >
                <strong>{item.label}</strong>
                <span>{item.description}</span>
                {!available && (
                  <span>
                    Not available: no compatible installed workflow is ready.
                  </span>
                )}
              </button>
            );
          })}
        </div>
      </section>
      {purpose === "demonstration" && (
        <section className={styles.section} aria-labelledby="run-demonstration">
          <h2 id="run-demonstration">2. Run a guided proof</h2>
          <p>
            Guided proofs are synthetic and deterministic. They run the shared
            pipeline without opening an account, using credentials, or creating
            a durable run.
          </p>
          <Link className={styles.linkButton} to="/guided-demonstrations">
            Open guided demonstrations
          </Link>
        </section>
      )}
      {purpose !== "" && purpose !== "demonstration" && (
        <section className={styles.section} aria-labelledby="run-strategy">
          <h2 id="run-strategy">2. Choose a strategy</h2>
          <p>These strategies are compatible with the selected purpose.</p>
          <div className={styles.cardGrid}>
            {strategies.map((choice) => (
              <button
                aria-pressed={strategyID === choice.strategy_id}
                className={styles.card}
                key={choice.strategy_id}
                onClick={() => onStrategy(choice.strategy_id)}
                type="button"
              >
                <strong>{choice.strategy_name}</strong>
                <span>{choice.cadence}</span>
              </button>
            ))}
          </div>
        </section>
      )}
      {strategyID !== "" && (
        <section className={styles.section} aria-labelledby="run-scope">
          <h2 id="run-scope">3. Choose exchange and instrument</h2>
          <p>
            These combinations are reviewed by the server for this strategy.
          </p>
          <div className={styles.cardGrid}>
            {compatibleChoices.map((choice) => (
              <button
                aria-pressed={selectedChoiceKey === choiceKey(choice)}
                className={styles.card}
                key={choiceKey(choice)}
                onClick={() => onChoice(choiceKey(choice))}
                type="button"
              >
                <strong>
                  {choice.instrument} on {choice.exchanges.join(" and ")}
                </strong>
                <span>{choice.warmup}</span>
              </button>
            ))}
          </div>
        </section>
      )}
      {selectedChoice && (
        <section className={styles.section} aria-labelledby="run-review">
          <h2 id="run-review">4. Review and start</h2>
          <article className={styles.card}>
            <div className={styles.cardHeader}>
              <h3>{selectedChoice.strategy_name}</h3>
              <span>
                {
                  purposes.find((item) => item.mode === selectedChoice.mode)
                    ?.label
                }
              </span>
            </div>
            <dl className={styles.facts}>
              <div>
                <dt>Scope</dt>
                <dd>
                  {selectedChoice.instrument} ·{" "}
                  {selectedChoice.exchanges.join(" and ")}
                </dd>
              </div>
              <div>
                <dt>Before it can start</dt>
                <dd>{selectedChoice.warmup}</dd>
              </div>
              <div>
                <dt>Execution boundary</dt>
                <dd>
                  {selectedChoice.order_capable
                    ? selectedChoice.mode === "sandbox"
                      ? "Uses the shared allocator, central risk, capped spot-order, accounting, and reconciliation pipeline. Starting still requires owner reauthentication and a current short-lived arm. Real-money production orders remain impossible."
                      : "Uses the shared simulated pipeline. It cannot place a real-money production order."
                    : "Advisory only. It cannot submit a transfer or order."}
                </dd>
              </div>
              <div>
                <dt>Input selection</dt>
                <dd>
                  The server will use its latest qualified immutable inputs and
                  reviewed portfolio, risk, fee, fill, latency, and slippage
                  assumptions.
                </dd>
              </div>
            </dl>
            <button
              className={styles.linkButton}
              disabled={createPending}
              onClick={() => onCreate(selectedChoice)}
              type="button"
            >
              {createPending ? "Starting reviewed run…" : "Start reviewed run"}
            </button>
          </article>
        </section>
      )}
    </>
  );
}
