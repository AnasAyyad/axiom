import { useEffect, useState } from "react";

import {
  getBuild,
  getReadiness,
  getStatus,
  type BuildInformation,
  type HealthResponse,
  type SystemStatus,
} from "../api/health";
import styles from "./HealthPage.module.css";

interface Snapshot {
  readonly build: BuildInformation;
  readonly readiness: HealthResponse;
  readonly system: SystemStatus;
}

type ViewState =
  | { readonly kind: "loading" }
  | { readonly kind: "loaded"; readonly snapshot: Snapshot }
  | { readonly kind: "error" };

/** HealthPage presents only authoritative backend health and build identity. */
export function HealthPage() {
  const [state, setState] = useState<ViewState>({ kind: "loading" });

  useEffect(() => {
    let active = true;
    Promise.all([getBuild(), getReadiness(), getStatus()])
      .then(([build, readiness, system]) => {
        if (active)
          setState({ kind: "loaded", snapshot: { build, readiness, system } });
      })
      .catch(() => {
        if (active) setState({ kind: "error" });
      });
    return () => {
      active = false;
    };
  }, []);

  return (
    <main className={styles.page}>
      <div className={styles.lockBanner} role="status" aria-live="polite">
        REAL TRADING DISABLED
      </div>
      <header className={styles.header}>
        <p className={styles.eyebrow}>Axiom V1A · Phase A1</p>
        <h1>System health</h1>
        <p>Public-data research and simulation skeleton</p>
      </header>
      {state.kind === "loading" && (
        <p aria-live="polite">Checking dependencies…</p>
      )}
      {state.kind === "error" && (
        <section className={styles.card} aria-labelledby="health-error">
          <h2 id="health-error">Health unavailable</h2>
          <p>
            The API response was missing, invalid, or unsafe. No capability was
            enabled.
          </p>
        </section>
      )}
      {state.kind === "loaded" && <HealthSnapshot snapshot={state.snapshot} />}
    </main>
  );
}

function HealthSnapshot({ snapshot }: { readonly snapshot: Snapshot }) {
  const ready = snapshot.readiness.status === "ready";
  return (
    <section className={styles.grid} aria-label="Axiom health details">
      <article className={styles.card}>
        <h2>Readiness</h2>
        <p className={ready ? styles.good : styles.warning}>
          {snapshot.readiness.status}
        </p>
        <dl>
          <div>
            <dt>Role</dt>
            <dd>{snapshot.readiness.role}</dd>
          </div>
          <div>
            <dt>Lifecycle</dt>
            <dd>{snapshot.system.lifecycle_state}</dd>
          </div>
          <div>
            <dt>Strategy activation</dt>
            <dd>{snapshot.system.strategy_activation}</dd>
          </div>
        </dl>
      </article>
      <article className={styles.card}>
        <h2>Build</h2>
        <dl>
          <div>
            <dt>Version</dt>
            <dd>{snapshot.build.version}</dd>
          </div>
          <div>
            <dt>Commit</dt>
            <dd>{snapshot.build.commit}</dd>
          </div>
          <div>
            <dt>Go</dt>
            <dd>{snapshot.build.go_version}</dd>
          </div>
        </dl>
      </article>
    </section>
  );
}
