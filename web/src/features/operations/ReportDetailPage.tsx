import { useMutation, useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { reportDetailQuery, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { hasAccess } from "../shared/access";
import { downloadArtifact } from "./artifactDownload";
import { reportLabel } from "./reportModel";
import styles from "../shared/ConsoleSurface.module.css";

export function ReportDetailPage() {
  const { id = "" } = useParams();
  const report = useQuery(reportDetailQuery(id));
  const session = useQuery(sessionQuery);
  const exportReport = useMutation({
    mutationFn: (format: APIModel<"ExportRequest">["format"]) =>
      postAPI<"ExportArtifact">(
        "/api/v1/exports",
        {
          resource_type: "report",
          resource_id: id,
          expected_revision: report.data!.revision,
          format,
          reason: "Download completed report with immutable provenance",
        } satisfies APIModel<"ExportRequest">,
        newIdempotencyKey("report-export"),
      ),
    onSuccess: (artifact) => downloadArtifact(artifact, `axiom-report-${id}`),
  });
  if (report.isLoading || session.isLoading)
    return <StatePanel state="loading" />;
  if (report.isError || session.isError || !report.data || !session.data)
    return <StatePanel state="forbidden" />;
  const item = report.data;
  const canExport = hasAccess(session.data.user, ["artifacts.read"]);
  return (
    <Page
      title={reportLabel(item.report_type)}
      eyebrow={`Report ${item.id}`}
      description="Generated report identity and exact provenance. Content is available only through an audited, redacted artifact."
    >
      <div className={styles.rowHeader}>
        <StatusBadge value={item.state} />
        <Link className={styles.linkButton} to="/operations/reports">
          Back to reports
        </Link>
      </div>
      <div className={styles.twoColumn}>
        <article className={styles.card}>
          <h2>Lifecycle</h2>
          <dl className={styles.facts}>
            <div>
              <dt>Revision</dt>
              <dd>{item.revision}</dd>
            </div>
            <div>
              <dt>Created</dt>
              <dd>{item.created_at}</dd>
            </div>
            <div>
              <dt>Generated</dt>
              <dd>{item.generated_at ?? "Pending"}</dd>
            </div>
            <div>
              <dt>Scheduled</dt>
              <dd>{item.schedule_id ?? "On demand"}</dd>
            </div>
          </dl>
        </article>
        <article className={styles.card}>
          <h2>Provenance</h2>
          <dl className={styles.facts}>
            <div>
              <dt>Mode</dt>
              <dd>{item.provenance.mode}</dd>
            </div>
            <div>
              <dt>Confidence</dt>
              <dd>{item.provenance.confidence_tier}</dd>
            </div>
            <div>
              <dt>Maturity</dt>
              <dd>{item.provenance.maturity}</dd>
            </div>
            <div>
              <dt>Source revision</dt>
              <dd>{item.provenance.source_revision}</dd>
            </div>
          </dl>
        </article>
      </div>
      {canExport && item.state === "SUCCEEDED" && (
        <section
          className={styles.controlCard}
          aria-label="Audited report download"
        >
          <h2>Audited download</h2>
          <div className={styles.actions}>
            {(["txt", "csv", "json", "jsonl"] as const).map((format) => (
              <button
                className={styles.secondary}
                key={format}
                type="button"
                disabled={exportReport.isPending}
                onClick={() => exportReport.mutate(format)}
              >
                Download {format.toUpperCase()}
              </button>
            ))}
          </div>
        </section>
      )}
      {item.state !== "SUCCEEDED" && (
        <StatePanel
          state={item.state === "FAILED" ? "error" : "stale"}
          detail={item.failure_code ?? "Report generation has not completed."}
        />
      )}
      {exportReport.isError && (
        <p className={styles.error} role="alert">
          The artifact could not be created. Refresh the completed report
          revision and retry.
        </p>
      )}
      <p className={styles.notice} role="note">
        This evidence reports observed state; it does not assert profitability
        or replace cumulative readiness qualifications.
      </p>
      <EvidenceDetails
        summary="Immutable source, model, valuation, hash, and build-safe report evidence."
        value={item}
      />
    </Page>
  );
}
