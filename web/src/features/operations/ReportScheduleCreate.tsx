import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { reportLabel, reportTypes } from "./reportModel";
import styles from "../shared/D2.module.css";

const minutes = Array.from({ length: 60 }, (_, value) => value);
const hours = Array.from({ length: 24 }, (_, value) => value);
const weekdays = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
] as const;
const weekdayValues = weekdays.map((_, index) => index);

function selectedBoundedValue(
  raw: string,
  allowed: readonly number[],
  fallback: number,
) {
  return allowed.find((value) => value.toString() === raw) ?? fallback;
}

export function CreateReportSchedule() {
  const client = useQueryClient();
  const [reportType, setReportType] =
    useState<APIModel<"ReportScheduleRequest">["report_type"]>(
      "strategy_results",
    );
  const [frequency, setFrequency] =
    useState<APIModel<"ReportScheduleRequest">["frequency"]>("daily");
  const [minute, setMinute] = useState(0);
  const [hour, setHour] = useState(6);
  const [weekday, setWeekday] = useState(1);
  const [reason, setReason] = useState(
    "Create a deterministic UTC evidence report schedule",
  );
  const mutation = useMutation({
    mutationFn: () => {
      const body: APIModel<"ReportScheduleRequest"> = {
        expected_revision: "1",
        report_type: reportType,
        frequency,
        minute_utc: minute,
        reason: reason.trim(),
      };
      if (frequency !== "hourly") body.hour_utc = hour;
      if (frequency === "weekly") body.weekday_utc = weekday;
      return postAPI<"CommandAccepted">(
        "/api/v1/report-schedules",
        body,
        newIdempotencyKey("report-schedule-create"),
      );
    },
    onSuccess: () =>
      client.invalidateQueries({ queryKey: ["report-schedules"] }),
  });
  return (
    <section
      className={styles.controlCard}
      aria-label="Create UTC report schedule"
    >
      <h3>Create schedule</h3>
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
          Frequency
          <select
            value={frequency}
            onChange={(event) =>
              setFrequency(event.target.value as typeof frequency)
            }
          >
            <option value="hourly">Hourly</option>
            <option value="daily">Daily</option>
            <option value="weekly">Weekly</option>
          </select>
        </label>
        <BoundedTimeSelect
          label="Minute UTC"
          value={minute}
          allowed={minutes}
          onChange={setMinute}
        />
        {frequency !== "hourly" && (
          <BoundedTimeSelect
            label="Hour UTC"
            value={hour}
            allowed={hours}
            onChange={setHour}
          />
        )}
        {frequency === "weekly" && (
          <label className={styles.field}>
            Weekday UTC
            <select
              value={weekday}
              onChange={(event) =>
                setWeekday(
                  selectedBoundedValue(event.target.value, weekdayValues, 0),
                )
              }
            >
              {weekdays.map((day, index) => (
                <option key={day} value={index}>
                  {day}
                </option>
              ))}
            </select>
          </label>
        )}
        <label className={`${styles.field} ${styles.spanAll}`}>
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
        {mutation.isPending ? "Creating…" : "Create UTC schedule"}
      </button>
      {mutation.isError && (
        <p className={styles.error} role="alert">
          Schedule creation failed. Verify UTC values, quota, permission, and
          reason.
        </p>
      )}
      {mutation.isSuccess && (
        <p className={styles.success} role="status">
          Schedule command accepted. The list refreshes when durable state is
          visible.
        </p>
      )}
    </section>
  );
}

function BoundedTimeSelect({
  label,
  value,
  allowed,
  onChange,
}: {
  readonly label: string;
  readonly value: number;
  readonly allowed: readonly number[];
  readonly onChange: (value: number) => void;
}) {
  return (
    <label className={styles.field}>
      {label}
      <select
        value={value}
        onChange={(event) =>
          onChange(selectedBoundedValue(event.target.value, allowed, 0))
        }
      >
        {allowed.map((option) => (
          <option key={option} value={option}>
            {option.toString().padStart(2, "0")}
          </option>
        ))}
      </select>
    </label>
  );
}
