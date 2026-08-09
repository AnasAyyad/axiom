import { newIdempotencyKey, postAPI, type APIModel } from "../../api/client";
import { HighRiskAuthorizationForm } from "../shared/HighRiskAuthorizationForm";
import styles from "../shared/ConsoleSurface.module.css";

interface QualificationStartProps {
  readonly qualification: APIModel<"OwnerControlResource">;
  readonly sourceSHA: string;
  readonly setSourceSHA: (value: string) => void;
  readonly configurationHash: string;
  readonly setConfigurationHash: (value: string) => void;
  readonly imageDigest: string;
  readonly setImageDigest: (value: string) => void;
  readonly serverIdentity: string;
  readonly setServerIdentity: (value: string) => void;
  readonly refresh: () => Promise<unknown>;
}

export function QualificationStart(props: QualificationStartProps) {
  return (
    <div className={styles.grid}>
      <div className={styles.form}>
        <StartField
          label="Exact source SHA"
          value={props.sourceSHA}
          onChange={props.setSourceSHA}
        />
        <StartField
          label="Configuration SHA-256"
          value={props.configurationHash}
          onChange={props.setConfigurationHash}
        />
        <StartField
          label="Image digest (optional)"
          value={props.imageDigest}
          onChange={props.setImageDigest}
        />
        <StartField
          label="Server identity (optional)"
          value={props.serverIdentity}
          onChange={props.setServerIdentity}
        />
      </div>
      <HighRiskAuthorizationForm
        title="Owner formal-start authorization"
        purpose="qualification_start"
        expectedRevision={props.qualification.revision}
        confirmLabel="Start fail-closed preflight"
        disabled={
          props.sourceSHA.trim() === "" || props.configurationHash.trim() === ""
        }
        onAuthorized={async (authorizationToken, reason) => {
          await postAPI<"CommandAccepted">(
            "/api/v1/qualifications",
            {
              qualification_id: props.qualification.id,
              authorization_token: authorizationToken,
              expected_revision: props.qualification.revision,
              reason,
              source_sha: props.sourceSHA.trim(),
              configuration_hash: props.configurationHash.trim(),
              ...(props.imageDigest.trim() === ""
                ? {}
                : { image_digest: props.imageDigest.trim() }),
              ...(props.serverIdentity.trim() === ""
                ? {}
                : { server_identity: props.serverIdentity.trim() }),
            } satisfies APIModel<"QualificationStartRequest">,
            newIdempotencyKey("qualification-start"),
          );
          await props.refresh();
        }}
      />
    </div>
  );
}

function StartField({
  label,
  value,
  onChange,
}: {
  readonly label: string;
  readonly value: string;
  readonly onChange: (value: string) => void;
}) {
  return (
    <label className={styles.field}>
      {label}
      <input value={value} onChange={(event) => onChange(event.target.value)} />
    </label>
  );
}
