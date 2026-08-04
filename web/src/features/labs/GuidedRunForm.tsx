import {
  useState,
  type Dispatch,
  type FormEvent,
  type SetStateAction,
} from "react";

import styles from "../../app/Page.module.css";
import type { LabRunForm } from "./labModel";

interface GuidedRunFormProps {
  readonly kind: "backtest" | "replay";
  readonly form: LabRunForm;
  readonly setForm: Dispatch<SetStateAction<LabRunForm>>;
  readonly pending: boolean;
  readonly allowed: boolean;
  readonly submit: () => void;
}

export function GuidedRunForm({
  kind,
  form,
  setForm,
  pending,
  allowed,
  submit,
}: GuidedRunFormProps) {
  const [view, setView] = useState<"guided" | "advanced">("guided");
  function handle(event: FormEvent) {
    event.preventDefault();
    submit();
  }
  return (
    <section className={styles.card} aria-labelledby={`${kind}-definition`}>
      <div className={styles.cardHeading}>
        <div>
          <span className={styles.eyebrow}>Approved run definition</span>
          <h2 id={`${kind}-definition`}>Immutable research inputs</h2>
        </div>
        <div
          className={styles.tabs}
          role="tablist"
          aria-label="Run definition view"
        >
          {(["guided", "advanced"] as const).map((option) => (
            <button
              key={option}
              type="button"
              role="tab"
              aria-selected={view === option}
              onClick={() => setView(option)}
            >
              {option === "guided" ? "Guided" : "Advanced evidence"}
            </button>
          ))}
        </div>
      </div>
      {view === "guided" ? (
        <form className={styles.form} onSubmit={handle}>
          <Field
            label="Configuration revision ID"
            value={form.configuration}
            set={(configuration) =>
              setForm((current) => ({ ...current, configuration }))
            }
          />
          <Field
            label="Approved dataset manifest ID"
            value={form.dataset}
            set={(dataset) => setForm((current) => ({ ...current, dataset }))}
          />
          <Field
            label="Research generation ID"
            value={form.researchGeneration}
            set={(researchGeneration) =>
              setForm((current) => ({ ...current, researchGeneration }))
            }
          />
          <label>
            Strategy version
            <select
              value={form.strategy}
              onChange={(event) =>
                setForm((current) => ({
                  ...current,
                  strategy: event.target.value,
                }))
              }
            >
              <option value="trend.v1a.1">Trend v1a.1</option>
            </select>
          </label>
          <Field
            label="Root seed SHA-256"
            value={form.seed}
            set={(seed) => setForm((current) => ({ ...current, seed }))}
            pattern="[0-9a-f]{64}"
          />
          {kind === "replay" && (
            <label>
              Replay speed
              <select
                value={form.speed}
                onChange={(event) =>
                  setForm((current) => ({
                    ...current,
                    speed: event.target.value as LabRunForm["speed"],
                  }))
                }
              >
                <option value="original">Original timing (1x)</option>
                <option value="accelerated">Accelerated timing (10x)</option>
                <option value="maximum">Maximum deterministic speed</option>
              </select>
            </label>
          )}
          <button type="submit" disabled={pending || !allowed}>
            {pending
              ? "Persisting…"
              : kind === "backtest"
                ? "Launch backtest"
                : "Create replay"}
          </button>
          {!allowed && (
            <p className={styles.disclaimer}>
              Your role can inspect results but cannot create research runs.
            </p>
          )}
        </form>
      ) : (
        <div role="tabpanel">
          <p className={styles.notice}>
            These are evidence selectors, not an arbitrary simulator. The server
            executes only approved versioned identities.
          </p>
          <dl className={styles.facts}>
            <Fact
              label="Instrument universe and date window"
              value="Bound by the dataset manifest"
            />
            <Fact
              label="Starting capital and allocation"
              value="Bound by the configuration revision"
            />
            <Fact
              label="Fees, latency, fill and slippage"
              value="Bound by the run model namespace"
            />
            <Fact
              label="Risk policy and rounding"
              value="Bound by the configuration and code commit"
            />
            <Fact label="Randomness" value="Deterministic root seed SHA-256" />
            <Fact
              label="Walk-forward evidence"
              value="Reported only by a registered validation suite"
            />
          </dl>
        </div>
      )}
    </section>
  );
}

function Field({
  label,
  value,
  set,
  pattern,
}: {
  readonly label: string;
  readonly value: string;
  readonly set: (value: string) => void;
  readonly pattern?: string;
}) {
  return (
    <label>
      {label}
      <input
        required
        value={value}
        pattern={pattern}
        onChange={(event) => set(event.target.value.trim())}
      />
    </label>
  );
}

function Fact({
  label,
  value,
}: {
  readonly label: string;
  readonly value: string;
}) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}
