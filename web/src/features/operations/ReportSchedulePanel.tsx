import { useQuery } from "@tanstack/react-query";

import { reportSchedulesQuery } from "../../api/queries";
import { StatePanel } from "../../components/StatePanel";
import { CreateReportSchedule } from "./ReportScheduleCreate";
import { ReportScheduleCard } from "./ReportScheduleCard";
import styles from "../shared/ConsoleSurface.module.css";

export function ReportSchedulePanel({
  canControl,
}: {
  readonly canControl: boolean;
}) {
  const query = useQuery(reportSchedulesQuery);
  if (query.isLoading) return <StatePanel state="loading" />;
  if (query.isError || !query.data)
    return (
      <StatePanel
        state="error"
        detail="UTC report schedules are unavailable."
      />
    );
  return (
    <section aria-labelledby="report-schedules-title">
      <div className={styles.rowHeader}>
        <div>
          <h2 id="report-schedules-title">UTC schedules</h2>
          <p>
            Schedules use deterministic Coordinated Universal Time (UTC),
            including weekday boundaries.
          </p>
        </div>
      </div>
      {canControl && <CreateReportSchedule />}
      {query.data.items.length === 0 ? (
        <StatePanel
          state="empty"
          detail="No scheduled reports have been configured."
        />
      ) : (
        <div className={styles.cardGrid}>
          {query.data.items.map((schedule) => (
            <ReportScheduleCard
              key={schedule.id}
              schedule={schedule}
              canControl={canControl}
            />
          ))}
        </div>
      )}
    </section>
  );
}
