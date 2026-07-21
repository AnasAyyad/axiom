import { useQuery } from "@tanstack/react-query";
import { useState } from "react";

import { auditQueryForType } from "../api/queries";
import { DataTable } from "../components/DataTable";
import { StatePanel } from "../components/StatePanel";
import { Page } from "./OperationalShared";
import styles from "./Page.module.css";

export function AuditPage() {
  const [eventType, setEventType] = useState("");
  const [includeDetail, setIncludeDetail] = useState(false);
  const audit = useQuery(auditQueryForType(eventType));
  const detailedAudit = useQuery({
    ...auditQueryForType(eventType, true),
    enabled: includeDetail,
    retry: false,
  });
  if (audit.isLoading) return <StatePanel state="loading" />;
  if (audit.isError) return <StatePanel state="forbidden" />;
  const displayedAudit =
    includeDetail && detailedAudit.data ? detailedAudit.data : audit.data!;
  return (
    <Page
      title="Audit"
      eyebrow="Immutable administrative evidence"
      description="Authorized, redacted command and lifecycle history with correlation and causation identities."
    >
      <section className={`${styles.card} ${styles.form}`}>
        <label>
          Exact event type
          <input
            maxLength={80}
            placeholder="For example: command_completed"
            value={eventType}
            onChange={(event) => setEventType(event.target.value)}
          />
        </label>
        <button
          type="button"
          onClick={() => setIncludeDetail((current) => !current)}
        >
          {includeDetail
            ? "Use redacted events"
            : "Show authorized evidence hashes"}
        </button>
      </section>
      {includeDetail && detailedAudit.isError && (
        <StatePanel
          state="forbidden"
          detail="The current role cannot inspect audit evidence identities."
        />
      )}
      {displayedAudit.items.length === 0 ? (
        <StatePanel
          state="empty"
          detail={
            eventType === ""
              ? "No audit events are available."
              : `No audit events match “${eventType}”.`
          }
        />
      ) : (
        <DataTable
          caption="Administrative audit events"
          rows={displayedAudit.items.map((item) => ({ ...item }))}
          columns={[
            { key: "recorded_at", label: "Recorded UTC" },
            { key: "event_type", label: "Action" },
            { key: "actor", label: "Actor" },
            { key: "correlation_id", label: "Correlation" },
            { key: "redacted", label: "Redacted" },
            { key: "safe_detail", label: "Authorized evidence" },
          ]}
        />
      )}
    </Page>
  );
}
