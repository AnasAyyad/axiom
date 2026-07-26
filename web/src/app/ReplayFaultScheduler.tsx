import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";

import {
  getAPI,
  newIdempotencyKey,
  postAPI,
  type APIModel,
} from "../api/client";
import { StatePanel } from "../components/StatePanel";
import styles from "./Page.module.css";

export function ReplayFaultScheduler({
  jobID,
  jobState,
}: {
  readonly jobID: string;
  readonly jobState: APIModel<"JobResource">["state"];
}) {
  const [fault, setFault] = useState({
    kind: "latency",
    ordinal: "1",
    delayNanos: "1000000",
    reason: "exercise deterministic replay recovery",
  });
  const faults = useQuery({
    queryKey: ["replay-faults", jobID],
    queryFn: () => getAPI<"ReplayFaultPage">(`/api/v1/replays/${jobID}/faults`),
  });
  const scheduleFault = useMutation({
    mutationFn: () =>
      postAPI<"ReplayFaultResource">(
        `/api/v1/replays/${jobID}/faults`,
        {
          fault: fault.kind,
          ordinal: fault.ordinal,
          delay_nanos: fault.kind === "latency" ? fault.delayNanos : "0",
          expected_revision: faults.data?.revision ?? "0",
          reason: fault.reason,
          repeatable: false,
        },
        newIdempotencyKey("replay-fault"),
      ),
    onSuccess: async () => {
      await faults.refetch();
    },
  });
  return (
    <section className={styles.card}>
      <h2>Simulation-only fault schedule</h2>
      <p className={styles.disclaimer}>
        Faults are immutable, ordinal-keyed, and accepted only while the replay
        is queued. They never touch live exchange connectivity.
      </p>
      <form
        className={styles.form}
        onSubmit={(event) => {
          event.preventDefault();
          scheduleFault.mutate();
        }}
      >
        <label>
          Fault
          <select
            value={fault.kind}
            onChange={(event) =>
              setFault({ ...fault, kind: event.target.value })
            }
          >
            {[
              "latency",
              "disconnect",
              "sequence_gap",
              "rejection",
              "partial_fill",
              "cancel_fill_race",
              "unknown_state",
              "storage_failure",
              "restart_at_event",
            ].map((kind) => (
              <option key={kind} value={kind}>
                {kind.replaceAll("_", " ")}
              </option>
            ))}
          </select>
        </label>
        <label>
          Event ordinal
          <input
            required
            inputMode="numeric"
            pattern="[1-9][0-9]*"
            value={fault.ordinal}
            onChange={(event) =>
              setFault({ ...fault, ordinal: event.target.value })
            }
          />
        </label>
        {fault.kind === "latency" && (
          <label>
            Delay nanoseconds
            <input
              required
              inputMode="numeric"
              pattern="[1-9][0-9]*"
              value={fault.delayNanos}
              onChange={(event) =>
                setFault({ ...fault, delayNanos: event.target.value })
              }
            />
          </label>
        )}
        <label>
          Audit reason
          <input
            required
            minLength={8}
            maxLength={500}
            value={fault.reason}
            onChange={(event) =>
              setFault({ ...fault, reason: event.target.value })
            }
          />
        </label>
        <button
          type="submit"
          disabled={scheduleFault.isPending || jobState !== "QUEUED"}
        >
          Schedule fault
        </button>
      </form>
      {scheduleFault.isError && (
        <StatePanel
          state="error"
          detail="The replay is no longer queued, the revision changed, or the schedule conflicts."
        />
      )}
      {faults.data && faults.data.items.length > 0 && (
        <ol className={styles.timeline} aria-label="Scheduled replay faults">
          {faults.data.items.map((item) => (
            <li key={item.id}>
              <strong>
                {item.fault.replaceAll("_", " ")} at ordinal {item.ordinal}
              </strong>
              <span>
                delay {item.delay_nanos} ns · revision {item.revision}
              </span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
