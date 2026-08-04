import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";

import {
  APIError,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../../api/client";
import { d1CollectionQuery, sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { hasAccess } from "../shared/access";
import { stringAttribute } from "../strategies/strategyModel";
import { ReportSchedulePanel } from "./ReportSchedulePanel";
import { reportLabel, reportTypes } from "./reportModel";
import styles from "../shared/D2.module.css";

export function ReportCenterPage() {
  const session = useQuery(sessionQuery);
  const query = useQuery(d1CollectionQuery("reports"));
  if (session.isLoading || query.isLoading)
    return <StatePanel state="loading" />;
  if (
    (session.error instanceof APIError && session.error.status === 403) ||
    (query.error instanceof APIError && query.error.status === 403)
  )
    return <StatePanel state="forbidden" />;
  if (session.isError || query.isError || !session.data || !query.data)
    return <StatePanel state="error" detail="Report state is unavailable." />;
  const canCreate = hasAccess(session.data.user, ["research.control"]);
  return (
    <Page
      title="Report Center"
      eyebrow="On-demand and scheduled evidence"
      description="Generate deterministic reports with immutable source, model, valuation, maturity, and generation provenance."
    >
      {query.isFetching && (
        <StatePanel
          state="stale"
          detail="Showing the prior report snapshot while durable state refreshes."
        />
      )}
      {canCreate && <CreateReport />}
      <section aria-labelledby="report-history-title">
        <h2 id="report-history-title">Report history</h2>
        {query.data.items.length === 0 ? (
          <StatePanel
            state="empty"
            detail="No report jobs have been created."
          />
        ) : (
          <div className={styles.cardGrid}>
            {query.data.items.map((report) => {
              const reportID = stringAttribute(
                report.attributes,
                "report_id",
                "",
              );
              return (
                <article className={styles.card} key={report.id}>
                  <div className={styles.cardHeader}>
                    <h3>
                      {reportLabel(
                        stringAttribute(
                          report.attributes,
                          "job_type",
                          report.id,
                        ).replace("report:", ""),
                      )}
                    </h3>
                    <StatusBadge value={report.state} />
                  </div>
                  <dl className={styles.facts}>
                    <div>
                      <dt>Revision</dt>
                      <dd>{report.revision}</dd>
                    </div>
                    <div>
                      <dt>Updated</dt>
                      <dd>{report.occurred_at ?? "Unavailable"}</dd>
                    </div>
                    <div>
                      <dt>Confidence</dt>
                      <dd>
                        {stringAttribute(
                          report.attributes,
                          "confidence_tier",
                          "Pending",
                        )}
                      </dd>
                    </div>
                  </dl>
                  {reportID !== "" && (
                    <Link
                      className={styles.linkButton}
                      to={`/operations/reports/${encodeURIComponent(reportID)}`}
                    >
                      Open report evidence
                    </Link>
                  )}
                  <EvidenceDetails
                    summary="Job identity and allowlisted report provenance."
                    value={report}
                  />
                </article>
              );
            })}
          </div>
        )}
      </section>
      <ReportSchedulePanel canControl={canCreate} />
      <p className={styles.notice} role="note">
        Strategy viability and platform readiness are separate. Historical,
        replay, shadow, Testnet, and Demo results do not prove profitability.
      </p>
    </Page>
  );
}

function CreateReport() {
  const client = useQueryClient();
  const [reportType, setReportType] =
    useState<APIModel<"ReportRequest">["report_type"]>("strategy_results");
  const [reason, setReason] = useState(
    "Create an on-demand provenance-preserving operational report",
  );
  const mutation = useMutation({
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
    onSuccess: () => client.invalidateQueries({ queryKey: ["d1", "reports"] }),
  });
  return (
    <section className={styles.controlCard} aria-label="Create report">
      <h2>Create an on-demand report</h2>
      <div className={styles.form}>
        <label className={styles.field}>
          Report type
          <select
            value={reportType}
            onChange={(event) =>
              setReportType(event.target.value as typeof reportType)
            }
          >
            {reportTypes.map((type) => (
              <option key={type} value={type}>
                {reportLabel(type)}
              </option>
            ))}
          </select>
        </label>
        <label className={styles.field}>
          Reason
          <textarea
            minLength={8}
            value={reason}
            onChange={(event) => setReason(event.target.value)}
          />
        </label>
      </div>
      <button
        className={styles.button}
        type="button"
        disabled={mutation.isPending || reason.trim().length < 8}
        onClick={() => mutation.mutate()}
      >
        {mutation.isPending ? "Queueing…" : "Create report"}
      </button>
      {mutation.isError && (
        <p className={styles.error} role="alert">
          Report creation was rejected. Check quota, reason, and permission.
        </p>
      )}
      {mutation.isSuccess && (
        <p className={styles.success} role="status">
          Report command accepted and queued durably.
        </p>
      )}
    </section>
  );
}
