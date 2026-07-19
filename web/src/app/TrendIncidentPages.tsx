import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";

import { getAPI } from "../api/client";
import {
  auditQuery,
  decisionsQuery,
  incidentsQuery,
  trendQuery,
} from "../api/queries";
import { DataTable } from "../components/DataTable";
import { MetricCard } from "../components/MetricCard";
import { StatePanel } from "../components/StatePanel";
import { Facts, Page } from "./OperationalShared";
import styles from "./Page.module.css";

export function TrendPage() {
  const trend = useQuery(trendQuery);
  const decisions = useQuery(decisionsQuery);
  if (trend.isLoading || decisions.isLoading)
    return <StatePanel state="loading" />;
  if (trend.isError || decisions.isError)
    return <StatePanel state="degraded" />;
  const trendData = trend.data!;
  const decisionData = decisions.data!;
  return (
    <Page
      title="Trend Strategy"
      eyebrow="Completed 4h candles"
      description="Versioned parameters, decision explanations, and local evidence maturity without a profitability claim."
    >
      <div className={styles.metrics}>
        <MetricCard label="Version" value={trendData.version} />
        <MetricCard label="Health" value={trendData.health} />
        <MetricCard label="Evidence" value={trendData.evidence_maturity} />
        <MetricCard
          label="Viability"
          value={trendData.viability ?? "undetermined"}
        />
      </div>
      <DataTable
        caption="Immutable strategy parameters"
        rows={trendData.parameters.map((item) => ({ ...item }))}
        columns={[
          { key: "id", label: "Parameter" },
          { key: "value", label: "Value" },
          { key: "unit", label: "Unit" },
          { key: "cadence", label: "Cadence" },
          { key: "mutability", label: "Mutability" },
        ]}
      />
      {decisionData.items.length === 0 ? (
        <StatePanel state="empty" detail="No durable Trend decisions yet." />
      ) : (
        <DataTable
          caption="Decision and rejection evidence"
          rows={decisionData.items.map((item) => ({ ...item }))}
          columns={[
            { key: "occurred_at", label: "UTC time" },
            { key: "outcome", label: "Outcome" },
            { key: "reason_code", label: "Reason" },
            { key: "market_view_id", label: "Market view" },
            { key: "revision", label: "Revision" },
          ]}
        />
      )}
    </Page>
  );
}

export function IncidentPage() {
  const incidents = useQuery(incidentsQuery);
  if (incidents.isLoading) return <StatePanel state="loading" />;
  if (incidents.isError) return <StatePanel state="forbidden" />;
  const incidentData = incidents.data!;
  return (
    <Page
      title="Incidents"
      eyebrow="Correlated evidence"
      description="Redacted immutable timelines link operational failures to deterministic replay windows."
    >
      {incidentData.items.length === 0 ? (
        <StatePanel state="empty" detail="No open or historical incidents." />
      ) : (
        <DataTable
          caption="Incident timeline"
          rows={incidentData.items.map((item) => ({ ...item }))}
          columns={[
            { key: "opened_at", label: "Opened UTC" },
            { key: "severity", label: "Severity" },
            { key: "state", label: "State" },
            { key: "reason_code", label: "Reason" },
            { key: "revision", label: "Revision" },
          ]}
        />
      )}
      {incidentData.items.length > 0 && (
        <p>
          <Link to={`/incidents/${incidentData.items[0]!.id}`}>
            Open latest incident evidence
          </Link>
        </p>
      )}
    </Page>
  );
}

export function IncidentDetailPage() {
  const { id = "" } = useParams();
  const incident = useQuery({
    queryKey: ["incident", id],
    queryFn: () => getAPI<"IncidentDetail">(`/api/v1/incidents/${id}`),
    enabled: id !== "",
  });
  if (incident.isLoading) return <StatePanel state="loading" />;
  if (incident.isError || !incident.data)
    return <StatePanel state="forbidden" />;
  const detail = incident.data;
  const replay = new URLSearchParams({
    incident: detail.id,
    dataset: detail.replay_window.dataset_id,
    first: detail.replay_window.first_ordinal,
    last: detail.replay_window.last_ordinal,
  });
  return (
    <Page
      title={`Incident ${detail.id}`}
      eyebrow={`${detail.severity} · ${detail.state}`}
      description="Authorized redacted evidence with an exact deterministic replay window."
    >
      <div className={styles.grid}>
        <Facts
          title="Incident"
          values={{
            Reason: detail.reason_code,
            "Opened UTC": detail.opened_at,
            Revision: detail.revision,
          }}
        />
        <Facts
          title="Replay window"
          values={{
            Dataset: detail.replay_window.dataset_id || "Unavailable",
            First: detail.replay_window.first_ordinal,
            Last: detail.replay_window.last_ordinal,
          }}
        />
      </div>
      <Link className={styles.action} to={`/replays?${replay.toString()}`}>
        Prepare incident replay
      </Link>
      {detail.timeline.length === 0 ? (
        <StatePanel state="empty" detail="No correlated timeline events." />
      ) : (
        <DataTable
          caption="Correlated incident timeline"
          rows={detail.timeline.map((item) => ({ ...item }))}
          columns={[
            { key: "occurred_at", label: "UTC time" },
            { key: "event_type", label: "Event" },
            { key: "correlation_id", label: "Correlation" },
            { key: "redacted", label: "Redacted" },
          ]}
        />
      )}
    </Page>
  );
}

export function AuditPage() {
  const audit = useQuery(auditQuery);
  if (audit.isLoading) return <StatePanel state="loading" />;
  if (audit.isError) return <StatePanel state="forbidden" />;
  return (
    <Page
      title="Audit"
      eyebrow="Immutable administrative evidence"
      description="Authorized, redacted command and lifecycle history with correlation and causation identities."
    >
      {audit.data!.items.length === 0 ? (
        <StatePanel state="empty" />
      ) : (
        <DataTable
          caption="Administrative audit events"
          rows={audit.data!.items.map((item) => ({ ...item }))}
          columns={[
            { key: "recorded_at", label: "Recorded UTC" },
            { key: "event_type", label: "Action" },
            { key: "actor", label: "Actor" },
            { key: "correlation_id", label: "Correlation" },
            { key: "redacted", label: "Redacted" },
          ]}
        />
      )}
    </Page>
  );
}
