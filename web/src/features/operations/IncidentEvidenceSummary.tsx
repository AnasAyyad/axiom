import { Link } from "react-router";

import type { APIModel } from "../../api/client";
import styles from "../shared/D2.module.css";

export function IncidentFacts({
  incident,
}: {
  readonly incident: APIModel<"IncidentDetail">;
}) {
  return (
    <article className={styles.card}>
      <h2>Incident</h2>
      <dl className={styles.facts}>
        <div>
          <dt>Reason</dt>
          <dd>{incident.reason_code}</dd>
        </div>
        <div>
          <dt>Owner</dt>
          <dd>{incident.owner_user_id}</dd>
        </div>
        <div>
          <dt>Opened</dt>
          <dd>{incident.opened_at}</dd>
        </div>
        <div>
          <dt>Updated</dt>
          <dd>{incident.updated_at}</dd>
        </div>
        <div>
          <dt>Revision</dt>
          <dd>{incident.revision}</dd>
        </div>
        <div>
          <dt>Resolution</dt>
          <dd>{incident.resolution_evidence ?? "Not resolved"}</dd>
        </div>
      </dl>
    </article>
  );
}

export function IncidentReplayFacts({
  incident,
}: {
  readonly incident: APIModel<"IncidentDetail">;
}) {
  const available =
    incident.replay_window.dataset_id !== "" &&
    incident.replay_window.first_ordinal !== "" &&
    incident.replay_window.last_ordinal !== "";
  const query = new URLSearchParams({
    incident: incident.id,
    dataset: incident.replay_window.dataset_id,
    first: incident.replay_window.first_ordinal,
    last: incident.replay_window.last_ordinal,
  });
  return (
    <article className={styles.card}>
      <h2>Deterministic replay input</h2>
      <dl className={styles.facts}>
        <div>
          <dt>Dataset</dt>
          <dd>{incident.replay_window.dataset_id || "Unavailable"}</dd>
        </div>
        <div>
          <dt>Ordinal range</dt>
          <dd>
            {available
              ? `${incident.replay_window.first_ordinal}–${incident.replay_window.last_ordinal}`
              : "Unavailable"}
          </dd>
        </div>
        <div>
          <dt>Source identity</dt>
          <dd>{incident.replay_window.source_identity ?? "Unavailable"}</dd>
        </div>
      </dl>
      {available ? (
        <Link className={styles.linkButton} to={`/replays?${query.toString()}`}>
          Prepare incident replay
        </Link>
      ) : (
        <p>No qualified input window is linked yet.</p>
      )}
    </article>
  );
}

export function IncidentRelations({
  incident,
}: {
  readonly incident: APIModel<"IncidentDetail">;
}) {
  return (
    <div className={styles.threeColumn}>
      <article className={styles.card}>
        <h2>Related alerts</h2>
        {incident.related_alert_ids.length === 0 ? (
          <p>None linked.</p>
        ) : (
          incident.related_alert_ids.map((id) => (
            <p key={id}>
              <Link to={`/operations/alerts/${encodeURIComponent(id)}`}>
                {id}
              </Link>
            </p>
          ))
        )}
      </article>
      <article className={styles.card}>
        <h2>Related activity</h2>
        {incident.related_activity_ids.length === 0 ? (
          <p>None linked.</p>
        ) : (
          incident.related_activity_ids.map((id) => (
            <p key={id}>
              <Link to="/activity/system-events">{id}</Link>
            </p>
          ))
        )}
      </article>
      <article className={styles.card}>
        <h2>Evidence holds</h2>
        {incident.evidence_holds.length === 0 ? (
          <p>None active.</p>
        ) : (
          incident.evidence_holds.map((hold) => (
            <p key={hold.id}>
              {hold.artifact_id} · {hold.created_at}
            </p>
          ))
        )}
      </article>
    </div>
  );
}
