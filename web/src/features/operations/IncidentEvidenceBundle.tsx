import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { EvidenceDetails } from "../shared/EvidenceDetails";
import { HighRiskAuthorizationForm } from "../shared/HighRiskAuthorizationForm";
import { downloadArtifact } from "./artifactDownload";
import styles from "../shared/ConsoleSurface.module.css";

export function IncidentEvidenceBundle({
  incident,
  canHold,
}: {
  readonly incident: APIModel<"IncidentDetail">;
  readonly canHold: boolean;
}) {
  const client = useQueryClient();
  const [format, setFormat] =
    useState<APIModel<"EvidenceBundleRequest">["format"]>("json");
  const [artifact, setArtifact] = useState<APIModel<"ExportArtifact"> | null>(
    null,
  );
  const bundle = useMutation({
    mutationFn: () =>
      postAPI<"ExportArtifact">(
        `/api/v1/incidents/${encodeURIComponent(incident.id)}/evidence-bundles`,
        {
          format,
          expected_revision: incident.revision,
          reason:
            "Create redacted incident evidence bundle for authorized review",
        } satisfies APIModel<"EvidenceBundleRequest">,
        newIdempotencyKey("incident-evidence"),
      ),
    onSuccess: (value) => {
      setArtifact(value);
      downloadArtifact(value, `axiom-incident-${incident.id}`);
    },
  });
  return (
    <section
      className={styles.controlCard}
      aria-label="Incident evidence bundle"
    >
      <h2>Evidence bundle</h2>
      <p>
        Artifacts are redacted, hash-sealed, audited, and expire after seven
        days unless an Owner/Admin applies a documented hold.
      </p>
      <label className={styles.field}>
        Format
        <select
          value={format}
          onChange={(event) => setFormat(event.target.value as typeof format)}
        >
          <option value="txt">TXT</option>
          <option value="csv">CSV</option>
          <option value="json">JSON</option>
          <option value="jsonl">JSONL</option>
        </select>
      </label>
      <button
        className={styles.button}
        type="button"
        disabled={bundle.isPending}
        onClick={() => bundle.mutate()}
      >
        {bundle.isPending ? "Creating…" : "Create and download bundle"}
      </button>
      {bundle.isError && (
        <p className={styles.error} role="alert">
          Evidence bundle creation failed. Refresh the incident revision and
          retry.
        </p>
      )}
      {artifact && (
        <>
          <EvidenceDetails
            summary="Artifact hash, expiry, revision, and redaction identity."
            value={artifact}
          />
          {canHold && !artifact.held && (
            <HighRiskAuthorizationForm
              title="Protect artifact with incident hold"
              purpose="artifact_hold"
              expectedRevision={artifact.revision}
              confirmLabel="Apply incident hold"
              onAuthorized={(token, reason) =>
                postAPI<"CommandAccepted">(
                  `/api/v1/exports/${encodeURIComponent(artifact.id)}/holds`,
                  {
                    hold_type: "incident",
                    reference_id: incident.id,
                    authorization_token: token,
                    expected_revision: artifact.revision,
                    reason,
                  } satisfies APIModel<"ArtifactHoldRequest">,
                  newIdempotencyKey("artifact-hold"),
                ).then((result) => {
                  void client.invalidateQueries({
                    queryKey: ["incident", incident.id],
                  });
                  return result;
                })
              }
            />
          )}
        </>
      )}
    </section>
  );
}
