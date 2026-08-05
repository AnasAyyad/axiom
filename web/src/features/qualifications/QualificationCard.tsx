import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { ConfirmAction } from "../../components/ConfirmAction";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { stringAttribute } from "../strategies/strategyModel";
import styles from "../shared/ConsoleSurface.module.css";
import { QualificationStart } from "./QualificationStart";

interface QualificationCardProps {
  readonly qualification: APIModel<"D1Resource">;
  readonly canStart: boolean;
  readonly canAbort: boolean;
}

export function QualificationCard({
  qualification,
  canStart,
  canAbort,
}: QualificationCardProps) {
  const queryClient = useQueryClient();
  const [sourceSHA, setSourceSHA] = useState("");
  const [configurationHash, setConfigurationHash] = useState("");
  const [imageDigest, setImageDigest] = useState("");
  const [serverIdentity, setServerIdentity] = useState("");
  const [abortReason, setAbortReason] = useState(
    "Operator requested a safe qualification abort",
  );
  const latestRun = qualification.attributes.latest_run_id;
  const runID =
    typeof latestRun === "string" && latestRun !== "" ? latestRun : undefined;
  const isAvailable = qualification.state === "AVAILABLE";
  const isActive = ["PREFLIGHT", "QUEUED", "RUNNING"].includes(
    qualification.state,
  );
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ["d1", "qualifications"] });
  const abort = useMutation({
    mutationFn: () => {
      if (!runID || abortReason.trim().length < 8)
        throw new Error("run_and_reason_required");
      return postAPI<"CommandAccepted">(
        `/api/v1/qualifications/${encodeURIComponent(runID)}/abort`,
        {
          expected_revision: qualification.revision,
          reason: abortReason.trim(),
        } satisfies APIModel<"RevisionCommandRequest">,
        newIdempotencyKey("qualification-abort"),
      );
    },
    onSuccess: refresh,
  });
  return (
    <article className={styles.card}>
      <QualificationSummary qualification={qualification} runID={runID} />
      <div
        className={styles.reasonCard}
        data-severity={qualification.state === "FAILED" ? "critical" : "info"}
      >
        <h3>
          {isAvailable
            ? "Ready for exact preflight"
            : "Current operational impact"}
        </h3>
        <p>
          {isAvailable
            ? "Starting creates a durable PREFLIGHT run; the official clock waits for every immutable requirement."
            : isActive
              ? "Monitor evidence continuously or abort when safety or declared conditions fail."
              : "The retained terminal state cannot be rewritten from failure into pass."}
        </p>
      </div>
      {isAvailable && canStart && (
        <QualificationStart
          qualification={qualification}
          sourceSHA={sourceSHA}
          setSourceSHA={setSourceSHA}
          configurationHash={configurationHash}
          setConfigurationHash={setConfigurationHash}
          imageDigest={imageDigest}
          setImageDigest={setImageDigest}
          serverIdentity={serverIdentity}
          setServerIdentity={setServerIdentity}
          refresh={refresh}
        />
      )}
      {isAvailable && !canStart && (
        <p className={styles.heroNote}>
          Only Owner / Admin may start a formal qualification. Your role may
          monitor retained evidence.
        </p>
      )}
      {isActive && canAbort && runID && (
        <div className={styles.controlCard}>
          <label className={styles.field}>
            Abort reason
            <textarea
              value={abortReason}
              onChange={(event) => setAbortReason(event.target.value)}
            />
          </label>
          <ConfirmAction
            trigger={
              <button className={styles.danger} type="button">
                Abort qualification
              </button>
            }
            title="Abort this qualification?"
            description="Abort is durable and audited. A failed or aborted official run cannot become a pass."
            confirmLabel="Abort qualification"
            onConfirm={() => abort.mutate()}
          />
          {abort.isError && (
            <p className={styles.error} role="alert">
              Abort was not accepted. Refresh the exact run revision and retry.
            </p>
          )}
        </div>
      )}
      {qualification.links.sandbox && (
        <Link className={styles.linkButton} to="/operations/sandbox">
          Open sandbox evidence
        </Link>
      )}
      <EvidenceDetails
        title="Evidence identity and verdict detail"
        summary="Only registered, server-redacted qualification identity is shown."
        value={{ ...qualification, run_id: runID }}
      />
    </article>
  );
}

function QualificationSummary({
  qualification,
  runID,
}: {
  readonly qualification: APIModel<"D1Resource">;
  readonly runID: string | undefined;
}) {
  return (
    <>
      <div className={styles.cardHeader}>
        <div>
          <h2>
            {stringAttribute(
              qualification.attributes,
              "name",
              qualification.id,
            )}
          </h2>
          <p>
            {stringAttribute(
              qualification.attributes,
              "kind",
              "operational qualification",
            ).replaceAll("_", " ")}
          </p>
        </div>
        <StatusBadge value={qualification.state} />
      </div>
      <dl className={styles.facts}>
        <div>
          <dt>Definition</dt>
          <dd>{qualification.id}</dd>
        </div>
        <div>
          <dt>Expected revision</dt>
          <dd>{qualification.revision}</dd>
        </div>
        <div>
          <dt>Declared duration</dt>
          <dd>
            {String(
              qualification.attributes.duration_seconds ?? "Defined by runner",
            )}
          </dd>
        </div>
        <div>
          <dt>Latest run</dt>
          <dd>{runID ?? "Not started"}</dd>
        </div>
        <div>
          <dt>Profitability evidence</dt>
          <dd>No</dd>
        </div>
      </dl>
    </>
  );
}
