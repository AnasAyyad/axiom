import type { APIModel } from "../api/client";
import { StatePanel } from "../components/StatePanel";
import { SandboxFact } from "./SandboxFact";
import styles from "./SandboxOperationsPage.module.css";
import { sandboxEnvironmentName, yesNo } from "./sandboxPresentation";

export function SandboxReconciliationGrid({
  reconciliations,
}: {
  readonly reconciliations: APIModel<"SandboxReconciliation">[];
}) {
  return (
    <section aria-labelledby="sandbox-reconciliation-heading">
      <h2 id="sandbox-reconciliation-heading">
        Reconciliation, suspense, and quarantine
      </h2>
      {reconciliations.length === 0 ? (
        <StatePanel state="empty" detail="No reconciliation evidence yet." />
      ) : (
        <div className={styles.grid}>
          {reconciliations.map((item) => (
            <article className={styles.card} key={item.id}>
              <header>
                <h3>{sandboxEnvironmentName(item)}</h3>
                <strong data-state={item.state === "clean" ? "good" : "warn"}>
                  {item.state}
                </strong>
              </header>
              <p>
                Epoch {item.account_epoch} · suspense {item.suspense_count} ·
                quarantine {item.quarantine_count}
              </p>
              {item.differences.map((difference) => (
                <p key={difference.id}>
                  <strong>{difference.state}</strong> {difference.category} ·{" "}
                  {difference.classification}
                  {difference.quantity
                    ? ` · ${difference.quantity} ${difference.asset ?? ""}`
                    : ""}
                </p>
              ))}
              <a href={item.audit_url}>Open reconciliation audit</a>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

export function SandboxResetGrid({
  incidents,
}: {
  readonly incidents: APIModel<"SandboxResetIncident">[];
}) {
  return (
    <section aria-labelledby="sandbox-resets-heading">
      <h2 id="sandbox-resets-heading">Account-reset incidents</h2>
      {incidents.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No account-epoch reset incidents recorded."
        />
      ) : (
        <div className={styles.grid}>
          {incidents.map((incident) => (
            <article className={styles.card} key={incident.id}>
              <h3>
                {sandboxEnvironmentName(incident)} · epoch{" "}
                {incident.prior_epoch} → {incident.new_epoch}
              </h3>
              <p>
                {incident.state}. External adjustments remain isolated from
                strategy P&amp;L.
              </p>
              {incident.adjustments.map((adjustment) => (
                <p key={`${adjustment.asset}-${adjustment.recorded_at}`}>
                  {adjustment.quantity} {adjustment.asset} · P&amp;L effect{" "}
                  {yesNo(adjustment.pnl_effect)}
                </p>
              ))}
              <a href={incident.audit_url}>Open reset audit</a>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}

export function SandboxQualificationPanel({
  qualification,
}: {
  readonly qualification: APIModel<"SandboxQualificationStatus">;
}) {
  return (
    <section
      className={styles.card}
      aria-labelledby="sandbox-qualification-heading"
    >
      <header>
        <div>
          <span>Sandbox chaos and soak</span>
          <h2 id="sandbox-qualification-heading">{qualification.state}</h2>
        </div>
        <strong data-state={qualification.qualified ? "good" : "warn"}>
          {qualification.qualified ? "FORMALLY QUALIFIED" : "NOT QUALIFIED"}
        </strong>
      </header>
      <dl>
        <SandboxFact label="Mode" value={qualification.mode} />
        <SandboxFact
          label="Observed duration"
          value={`${qualification.observed_duration_seconds}s / ${qualification.required_duration_seconds}s`}
        />
        <SandboxFact
          label="Chaos"
          value={`${qualification.chaos.status} · ${qualification.chaos.passed} passed · ${qualification.chaos.failed} failed`}
        />
        <SandboxFact
          label="SLO samples"
          value={`${qualification.slo.samples} · ${qualification.slo.passing ? "passing" : "not passing"}`}
        />
        <SandboxFact
          label="Order integrity"
          value={`${qualification.slo.duplicate_creates} duplicate creates · ${qualification.slo.lost_fills} lost fills · ${qualification.slo.double_posted_fills} double-posted fills`}
        />
        <SandboxFact
          label="Recovery evidence"
          value={`${qualification.slo.reconnects} reconnects · ${qualification.slo.restarts} restarts · ${qualification.slo.recovery_duration_ms}ms max`}
        />
        <SandboxFact
          label="Critical alert latency"
          value={`${qualification.slo.critical_alert_latency_ms}ms max`}
        />
        <SandboxFact
          label="Memory trend"
          value={`${qualification.slo.resident_memory_delta_bytes} bytes · leak ${yesNo(qualification.slo.positive_memory_leak_trend)}`}
        />
        <SandboxFact
          label="Commit / build"
          value={`${qualification.commit_sha ?? "not recorded"} / ${qualification.build_hash ?? "not recorded"}`}
        />
        <SandboxFact
          label="Executable / image"
          value={`${qualification.executable_hash ?? "not recorded"} / ${qualification.image_hash ?? "not recorded"}`}
        />
        <SandboxFact
          label="Configuration"
          value={qualification.configuration_hash ?? "not recorded"}
        />
        <SandboxFact
          label="Profitability evidence"
          value={yesNo(qualification.profitability_evidence)}
        />
        <SandboxFact
          label="Formal soak pending"
          value={yesNo(qualification.formal_soak_pending)}
        />
        <SandboxFact
          label="Failures"
          value={qualification.failures.join(", ") || "none"}
        />
      </dl>
      <div aria-label="Sandbox qualification bounded recovery incidents">
        <h3>Bounded read-only recovery</h3>
        {qualification.recovery_incidents.length === 0 ? (
          <p>No permitted recovery incident observed.</p>
        ) : (
          qualification.recovery_incidents.map((incident) => (
            <p key={`${incident.account_id}-${incident.detected_at}`}>
              {incident.account_id} · {incident.exchange} /{" "}
              {incident.environment} · {incident.incident_source} ·{" "}
              {incident.state} · {incident.reason_category} /{" "}
              {incident.cause_code} · clean checks {incident.clean_check_count}{" "}
              · deadline {incident.deadline_at}
              {incident.recovery_timestamp
                ? ` · recovered ${incident.recovery_timestamp}`
                : ""}
            </p>
          ))
        )}
      </div>
      <p className={styles.disclaimer}>
        A smoke pass is never a 72-hour pass and test/demo liquidity is never
        profitability evidence.
      </p>
      <a href={qualification.audit_url}>Open qualification audit</a>
    </section>
  );
}
