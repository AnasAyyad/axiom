import { useMutation, useQueryClient } from "@tanstack/react-query";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { ConfirmAction } from "../../components/ConfirmAction";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { StatusBadge } from "../shared/StatusBadge";
import { reportLabel } from "./reportModel";
import styles from "../shared/ConsoleSurface.module.css";

export function ReportScheduleCard({
  schedule,
  canControl,
}: {
  readonly schedule: APIModel<"ReportSchedule">;
  readonly canControl: boolean;
}) {
  const client = useQueryClient();
  const nextState = schedule.state === "active" ? "paused" : "active";
  const mutation = useMutation({
    mutationFn: () =>
      postAPI<"CommandAccepted">(
        `/api/v1/report-schedules/${encodeURIComponent(schedule.id)}/transitions`,
        {
          expected_revision: schedule.revision,
          state: nextState,
          reason: `${nextState === "paused" ? "Pause" : "Resume"} UTC report schedule after operator review`,
        } satisfies APIModel<"ReportScheduleTransitionRequest">,
        newIdempotencyKey(`report-schedule-${nextState}`),
      ),
    onSuccess: () =>
      client.invalidateQueries({ queryKey: ["report-schedules"] }),
  });
  return (
    <article className={styles.card}>
      <div className={styles.cardHeader}>
        <h3>{reportLabel(schedule.report_type)}</h3>
        <StatusBadge value={schedule.state} />
      </div>
      <dl className={styles.facts}>
        <div>
          <dt>Frequency</dt>
          <dd>{schedule.frequency}</dd>
        </div>
        <div>
          <dt>Next run UTC</dt>
          <dd>{schedule.next_run_at}</dd>
        </div>
        <div>
          <dt>Last run UTC</dt>
          <dd>{schedule.last_run_at ?? "Not run"}</dd>
        </div>
        <div>
          <dt>Revision</dt>
          <dd>{schedule.revision}</dd>
        </div>
      </dl>
      {canControl && (
        <ConfirmAction
          trigger={
            <button className={styles.secondary} type="button">
              {nextState === "paused" ? "Pause" : "Resume"} schedule
            </button>
          }
          title={`${nextState === "paused" ? "Pause" : "Resume"} this schedule?`}
          description={`The command is audited and bound to revision ${schedule.revision}.`}
          confirmLabel={`${nextState === "paused" ? "Pause" : "Resume"} schedule`}
          onConfirm={() => mutation.mutate()}
        />
      )}
      {mutation.isError && (
        <p className={styles.error} role="alert">
          Transition rejected. Refresh the exact revision before retrying.
        </p>
      )}
      <EvidenceDetails
        summary="Schedule identity and deterministic UTC boundaries."
        value={schedule}
      />
    </article>
  );
}
