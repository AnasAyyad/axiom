import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";

import {
  getAPI,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../../api/client";
import { d1CollectionQuery } from "../../api/queries";
import { ConfirmAction } from "../../components/ConfirmAction";
import { DataTable } from "../../components/DataTable";
import { StatePanel } from "../../components/StatePanel";
import styles from "../../app/Page.module.css";
import { compareLabRuns, labDownloadName } from "./labModel";

interface LabRunToolsProps {
  readonly job: APIModel<"JobResource">;
  readonly canControl: boolean;
  readonly canExport: boolean;
  readonly refresh: () => Promise<unknown>;
}

export function LabRunTools({
  job,
  canControl,
  canExport,
  refresh,
}: LabRunToolsProps) {
  const client = useQueryClient();
  const [compareID, setCompareID] = useState("");
  const compare = useQuery({
    queryKey: ["lab-compare", job.kind, compareID],
    queryFn: () =>
      getAPI<"JobResource">(
        `/api/v1/${job.kind === "backtest" ? "backtests" : "replays"}/${encodeURIComponent(compareID)}`,
      ),
    enabled: compareID !== "",
  });
  const history = useQuery(d1CollectionQuery("lab-runs"));
  const control = useMutation({
    mutationFn: (action: "pause" | "resume" | "cancel" | "reproduce") =>
      postAPI<"CommandAccepted">(
        `/api/v1/lab-runs/${encodeURIComponent(job.id)}/${action}`,
        {
          expected_revision: job.revision,
          reason: `Authorized researcher requested ${action} for immutable lab run`,
        },
        newIdempotencyKey(`lab-${action}`),
      ),
    onSuccess: async () => {
      await refresh();
      await client.invalidateQueries({ queryKey: ["d1", "lab-runs"] });
    },
  });
  const exportRun = useMutation({
    mutationFn: (format: APIModel<"ExportRequest">["format"]) =>
      postAPI<"ExportArtifact">(
        "/api/v1/exports",
        {
          resource_type: "lab_run",
          resource_id: job.id,
          format,
          expected_revision: job.revision,
          reason:
            "Export immutable lab identity and safe reproduction evidence",
        } satisfies APIModel<"ExportRequest">,
        newIdempotencyKey("lab-export"),
      ),
    onSuccess: downloadArtifact,
  });
  const durableRuns =
    history.data?.items.filter((item) => {
      const jobType = String(item.attributes.job_type ?? "");
      return jobType === "backtest" || jobType === "replay";
    }) ?? [];
  return (
    <>
      <section className={styles.card} aria-label="Durable lab lifecycle">
        <h2>Lifecycle controls</h2>
        <div className={styles.actions}>
          {(job.kind === "replay"
            ? (["cancel", "reproduce"] as const)
            : (["pause", "resume", "cancel", "reproduce"] as const)
          ).map((action) => {
            const available = job.lifecycle?.[action] ?? false;
            return (
              <ConfirmAction
                key={action}
                trigger={
                  <button
                    type="button"
                    className={
                      action === "cancel"
                        ? styles.actionDanger
                        : styles.actionSecondary
                    }
                    disabled={!canControl || !available || control.isPending}
                  >
                    {action}
                  </button>
                }
                title={`${action} this durable run?`}
                description="The server audits the reason and checks the exact current revision before applying the state transition."
                confirmLabel={action}
                onConfirm={() => control.mutate(action)}
              />
            );
          })}
        </div>
        {!canControl && (
          <p className={styles.disclaimer}>
            Read-only access: research.control is required.
          </p>
        )}
        {control.isError && (
          <StatePanel
            state="error"
            detail="The control was rejected because the revision, state, quota, or permission changed."
          />
        )}
      </section>
      <section className={styles.card}>
        <h2>Audited reproduction export</h2>
        <p className={styles.disclaimer}>
          Artifacts are redacted, hash-sealed, audited, and retained for seven
          days unless held.
        </p>
        <div className={styles.actions} aria-label="Lab export formats">
          {(["txt", "csv", "json", "jsonl"] as const).map((format) => (
            <button
              key={format}
              type="button"
              className={styles.actionSecondary}
              disabled={!canExport || exportRun.isPending}
              onClick={() => exportRun.mutate(format)}
            >
              Download {format.toUpperCase()}
            </button>
          ))}
        </div>
        {job.reproduction_bundle ? (
          <details>
            <summary>Safe reproduction manifest</summary>
            <pre className={styles.canonical}>
              {JSON.stringify(job.reproduction_bundle, null, 2)}
            </pre>
          </details>
        ) : (
          <StatePanel
            state="loading"
            detail="The worker has not materialized the immutable run manifest yet."
          />
        )}
      </section>
      <section className={styles.card}>
        <h2>Compare exact run evidence</h2>
        <form
          className={styles.form}
          onSubmit={(event) => event.preventDefault()}
        >
          <label>
            Comparison run ID
            <input
              value={compareID}
              onChange={(event) => setCompareID(event.target.value.trim())}
            />
          </label>
        </form>
        {compare.data && (
          <DataTable
            caption={`Comparison: ${job.id} and ${compare.data.id}`}
            rows={compareLabRuns(job, compare.data)}
            columns={[
              { key: "field", label: "Evidence field" },
              { key: "left", label: job.id },
              { key: "right", label: compare.data.id },
              { key: "changed", label: "Changed" },
            ]}
          />
        )}
        {compare.isError && (
          <StatePanel
            state="error"
            detail="The comparison run was not found or is not available to this role."
          />
        )}
      </section>
      <section className={styles.card}>
        <h2>Recent durable runs</h2>
        {durableRuns.length ? (
          <ul className={styles.timeline}>
            {durableRuns.map((item) => {
              const jobType = String(item.attributes.job_type);
              const path = jobType === "replay" ? "replays" : "backtests";
              return (
                <li key={item.id}>
                  <Link to={`/${path}/${encodeURIComponent(item.id)}`}>
                    {item.id}
                  </Link>
                  <span>
                    {jobType} · {item.state} · revision {item.revision}
                  </span>
                </li>
              );
            })}
          </ul>
        ) : (
          <StatePanel
            state={history.isLoading ? "loading" : "empty"}
            detail="No durable lab runs are visible."
          />
        )}
      </section>
    </>
  );
}

function downloadArtifact(artifact: APIModel<"ExportArtifact">) {
  if (artifact.content === undefined) return;
  const href = URL.createObjectURL(
    new Blob([artifact.content], { type: artifact.content_type }),
  );
  const anchor = document.createElement("a");
  anchor.href = href;
  anchor.download = labDownloadName(artifact.resource_id, artifact.format);
  anchor.click();
  URL.revokeObjectURL(href);
}

export function LabSafetyNote() {
  return (
    <p className={styles.notice} role="note">
      <strong>Research evidence only.</strong> Strategy viability and platform
      readiness are separate. Historical, replay, shadow, or exchange-provided
      non-production results do not prove profitability.
    </p>
  );
}
