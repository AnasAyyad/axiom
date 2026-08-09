import { useMutation } from "@tanstack/react-query";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { safeDownloadName } from "./activityModel";
import styles from "../shared/ConsoleSurface.module.css";

interface ActivityDetailPanelProps {
  readonly activity: APIModel<"ActivityResource">;
  readonly canExport: boolean;
  readonly onCorrelation: (value: string) => void;
}

function downloadArtifact(
  artifact: APIModel<"ExportArtifact">,
  view: "decisions_orders" | "system_events",
) {
  if (artifact.content === undefined) return;
  const blob = new Blob([artifact.content], { type: artifact.content_type });
  const href = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = href;
  anchor.download = safeDownloadName(view, artifact.format);
  anchor.click();
  URL.revokeObjectURL(href);
}

export function ActivityDetailPanel({
  activity,
  canExport,
  onCorrelation,
}: ActivityDetailPanelProps) {
  const exportRecord = useMutation({
    mutationFn: (format: APIModel<"ExportRequest">["format"]) =>
      postAPI<"ExportArtifact">(
        "/api/v1/exports",
        {
          resource_type: "activity",
          resource_id: activity.id,
          expected_revision: activity.activity_revision,
          format,
          reason: "Download filtered activity result for authorized review",
        } satisfies APIModel<"ExportRequest">,
        newIdempotencyKey("activity-export"),
      ),
    onSuccess: (artifact) => downloadArtifact(artifact, activity.view),
  });
  return (
    <section className={styles.card} aria-label="Activity explanation">
      <div className={styles.rowHeader}>
        <div>
          <h2>{activity.reason.summary}</h2>
          <p>{activity.reason.explanation}</p>
        </div>
        <StatusBadge value={activity.reason.severity} />
      </div>
      <div
        className={styles.reasonCard}
        data-severity={activity.reason.severity}
      >
        <h3>Recommended action</h3>
        <p>{activity.reason.suggested_action}</p>
      </div>
      <dl className={styles.facts}>
        <div>
          <dt>Outcome</dt>
          <dd>{activity.outcome}</dd>
        </div>
        <div>
          <dt>Source</dt>
          <dd>
            {activity.source_type} / {activity.source_id}
          </dd>
        </div>
        <div>
          <dt>Occurred</dt>
          <dd>{activity.occurred_at}</dd>
        </div>
        <div>
          <dt>Correlation</dt>
          <dd>
            <button
              className={styles.tableButton}
              type="button"
              onClick={() => onCorrelation(activity.correlation_id)}
            >
              {activity.correlation_id}
            </button>
          </dd>
        </div>
      </dl>
      {canExport && (
        <div className={styles.actions} aria-label="Audited export formats">
          {(["txt", "csv", "json", "jsonl"] as const).map((format) => (
            <button
              className={styles.secondary}
              key={format}
              type="button"
              disabled={exportRecord.isPending}
              onClick={() => exportRecord.mutate(format)}
            >
              Download {format.toUpperCase()}
            </button>
          ))}
        </div>
      )}
      {exportRecord.isError && (
        <p className={styles.error} role="alert">
          The audited export could not be created. Check the revision and your
          artifact availability, then retry.
        </p>
      )}
      <EvidenceDetails
        summary="This server-redacted payload links the decision, risk, order, fill, or operational evidence without exposing private exchange data."
        value={{
          id: activity.id,
          revision: activity.activity_revision,
          reason_code: activity.reason.code,
          source_revision: activity.source_revision,
          causation_id: activity.causation_id,
          links: activity.links,
          details: activity.details,
        }}
      />
    </section>
  );
}
