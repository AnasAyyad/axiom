import { APIError } from "../api/client";
import { StatePanel } from "../components/StatePanel";
import styles from "./SandboxControls.module.css";

export function SandboxMutationResult({
  errors,
  pending,
  success,
}: {
  readonly errors: unknown[];
  readonly pending: boolean;
  readonly success: boolean;
}) {
  const error = errors.find((item) => item != null);
  if (pending)
    return <StatePanel state="loading" detail="Persisting audited command…" />;
  if (error instanceof APIError)
    return (
      <StatePanel
        state={error.status === 403 ? "forbidden" : "error"}
        detail={`Backend refusal: ${error.code} · ${error.correlationID}`}
      />
    );
  if (error instanceof Error)
    return <StatePanel state="error" detail={error.message} />;
  if (success)
    return (
      <p className={styles.success} role="status">
        Audited command accepted. Refreshing authoritative state.
      </p>
    );
  return null;
}
