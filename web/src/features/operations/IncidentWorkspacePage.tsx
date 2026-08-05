import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link, useParams } from "react-router";

import { getAPI } from "../../api/client";
import { sessionQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { DataTable } from "../../components/DataTable";
import { StatePanel } from "../../components/StatePanel";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { hasAccess } from "../shared/access";
import { IncidentControls } from "./IncidentControls";
import { IncidentEvidenceBundle } from "./IncidentEvidenceBundle";
import {
  IncidentFacts,
  IncidentRelations,
  IncidentReplayFacts,
} from "./IncidentEvidenceSummary";
import styles from "../shared/D2.module.css";

export function IncidentWorkspacePage() {
  const { id = "" } = useParams();
  const [includeRaw, setIncludeRaw] = useState(false);
  const session = useQuery(sessionQuery);
  const incident = useQuery({
    queryKey: ["incident", id, includeRaw],
    queryFn: () =>
      getAPI<"IncidentDetail">(
        `/api/v1/incidents/${encodeURIComponent(id)}${includeRaw ? "?include_raw=true" : ""}`,
      ),
    enabled: id !== "",
    retry: false,
  });
  if (incident.isLoading || session.isLoading)
    return <StatePanel state="loading" />;
  if (incident.isError || session.isError || !incident.data || !session.data)
    return <StatePanel state="forbidden" />;
  const canWrite = hasAccess(session.data.user, ["incident.write"]);
  const canExport = hasAccess(session.data.user, ["artifacts.read"]);
  const canHold = hasAccess(session.data.user, ["artifacts.manage"]);
  return (
    <Page
      title={`Incident ${incident.data.id}`}
      eyebrow={`${incident.data.severity} · ${incident.data.state}`}
      description="Hash-linked lifecycle evidence, ownership, remediation, related activity and alerts, exact replay inputs, evidence holds, and resolution proof."
    >
      <div className={styles.rowHeader}>
        <StatusBadge value={incident.data.state} />
        <Link className={styles.linkButton} to="/incidents">
          Back to incidents
        </Link>
      </div>
      <div className={styles.twoColumn}>
        <IncidentFacts incident={incident.data} />
        <IncidentReplayFacts incident={incident.data} />
      </div>
      <button
        className={styles.secondary}
        type="button"
        onClick={() => setIncludeRaw((value) => !value)}
      >
        {includeRaw
          ? "Use redacted evidence"
          : "Show authorized evidence hashes"}
      </button>
      {canWrite && incident.data.state !== "resolved" && (
        <IncidentControls incident={incident.data} />
      )}
      {canExport && (
        <IncidentEvidenceBundle incident={incident.data} canHold={canHold} />
      )}
      <IncidentRelations incident={incident.data} />
      <h2>Hash-linked timeline</h2>
      {incident.data.timeline.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No incident timeline events are visible."
        />
      ) : (
        <DataTable
          caption="Immutable incident timeline"
          rows={incident.data.timeline.map((item) => ({
            ...item,
            actor: item.actor ?? "system",
            reason: item.reason ?? "none",
            reference: item.reference_id ?? "none",
            event_hash: item.event_hash ?? "redacted",
          }))}
          columns={[
            { key: "occurred_at", label: "UTC time" },
            { key: "event_type", label: "Event" },
            { key: "actor", label: "Actor" },
            { key: "reason", label: "Reason" },
            { key: "reference", label: "Reference" },
            { key: "event_hash", label: "Evidence hash" },
          ]}
        />
      )}
      <EvidenceDetails
        summary="Incident evidence remains redacted unless this role is authorized for hashes and allowlisted detail."
        value={incident.data}
      />
    </Page>
  );
}
