import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import {
  APIError,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../../api/client";
import { d1CollectionQuery, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { hasAccess } from "../shared/access";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { stringAttribute } from "../strategies/strategyModel";
import styles from "../shared/D2.module.css";

const reportTypes: ReadonlyArray<APIModel<"ReportRequest">["report_type"]> = [
  "strategy_results",
  "decisions_orders",
  "portfolios",
  "inventory_pnl",
  "risk",
  "exchange_data_health",
  "lab_runs",
  "sandbox_qualifications",
  "platform_readiness",
];

export function ReportCenterPage() {
  const session = useQuery(sessionQuery);
  const query = useQuery(d1CollectionQuery("reports"));
  const queryClient = useQueryClient();
  const [reportType, setReportType] =
    useState<APIModel<"ReportRequest">["report_type"]>("strategy_results");
  const [reason, setReason] = useState(
    "Researcher requested an on-demand provenance-preserving report",
  );
  const create = useMutation({
    mutationFn: () =>
      postAPI<"CommandAccepted">(
        "/api/v1/reports",
        {
          expected_revision: "1",
          reason: reason.trim(),
          report_type: reportType,
        } satisfies APIModel<"ReportRequest">,
        newIdempotencyKey("report-create"),
      ),
    onSuccess: () =>
      queryClient.invalidateQueries({ queryKey: ["d1", "reports"] }),
  });
  if (session.isLoading || query.isLoading)
    return <StatePanel state="loading" />;
  if (
    (session.error instanceof APIError && session.error.status === 403) ||
    (query.error instanceof APIError && query.error.status === 403)
  )
    return <StatePanel state="forbidden" />;
  if (session.isError || query.isError || !session.data || !query.data)
    return <StatePanel state="error" detail="Report jobs are unavailable." />;
  const canCreate = hasAccess(session.data.user, ["research.control"]);
  return (
    <Page
      title="Report Center"
      eyebrow="On-demand report lifecycle"
      description="Create provenance-preserving report jobs and inspect durable state. Scheduled delivery and routing are completed in D4."
    >
      {canCreate && (
        <section className={styles.controlCard} aria-label="Create report">
          <h2>Create an on-demand report</h2>
          <div className={styles.form}>
            <label className={styles.field}>
              Report type
              <select
                value={reportType}
                onChange={(event) =>
                  setReportType(
                    event.target
                      .value as APIModel<"ReportRequest">["report_type"],
                  )
                }
              >
                {reportTypes.map((type) => (
                  <option key={type} value={type}>
                    {type.replaceAll("_", " ")}
                  </option>
                ))}
              </select>
            </label>
            <label className={styles.field}>
              Reason
              <textarea
                value={reason}
                onChange={(event) => setReason(event.target.value)}
              />
            </label>
          </div>
          <button
            className={styles.button}
            type="button"
            disabled={create.isPending || reason.trim().length < 8}
            onClick={() => create.mutate()}
          >
            {create.isPending ? "Queueing…" : "Create report"}
          </button>
          {create.isError && (
            <p className={styles.error} role="alert">
              Report creation was rejected. Check quota, reason, and permission.
            </p>
          )}
          {create.isSuccess && (
            <p className={styles.success} role="status">
              Report command accepted and queued durably.
            </p>
          )}
        </section>
      )}
      {query.data.items.length === 0 ? (
        <StatePanel state="empty" detail="No report jobs have been created." />
      ) : (
        <div className={styles.cardGrid}>
          {query.data.items.map((report) => (
            <article className={styles.card} key={report.id}>
              <div className={styles.cardHeader}>
                <h2>
                  {stringAttribute(report.attributes, "job_type", report.id)}
                </h2>
                <StatusBadge value={report.state} />
              </div>
              <dl className={styles.facts}>
                <div>
                  <dt>Revision</dt>
                  <dd>{report.revision}</dd>
                </div>
                <div>
                  <dt>Created</dt>
                  <dd>{report.occurred_at ?? "Unavailable"}</dd>
                </div>
                <div>
                  <dt>Correlation</dt>
                  <dd>{report.correlation_id}</dd>
                </div>
              </dl>
              <EvidenceDetails
                summary="Report identity and model/source provenance are server-redacted."
                value={report}
              />
            </article>
          ))}
        </div>
      )}
      <p className={styles.notice} role="note">
        Reports preserve mode, confidence, valuation/model provenance, maturity,
        source identity, generation time, and revision. They do not assert
        profitability.
      </p>
    </Page>
  );
}
