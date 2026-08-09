import { useQuery } from "@tanstack/react-query";

import { dataCatalogueQuery } from "../../api/queries";
import { Page } from "../../app/OperationalShared";
import { StatePanel } from "../../components/StatePanel";
import styles from "../shared/ConsoleSurface.module.css";

function displayDate(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

export function DataCataloguePage() {
  const catalogue = useQuery(dataCatalogueQuery);
  if (catalogue.isLoading) return <StatePanel state="loading" />;
  if (catalogue.isError || !catalogue.data)
    return (
      <StatePanel
        state="error"
        detail="The protected data catalogue is unavailable."
      />
    );
  return (
    <Page
      title="Data Catalogue"
      eyebrow="Protected, server-registered evidence"
      description="This inventory comes from validated server-side manifests. You can inspect coverage and hashes, but the browser cannot upload market dumps or choose a raw storage path."
    >
      {catalogue.data.items.length === 0 && (
        <StatePanel
          state="empty"
          detail="No registered data is available yet."
        />
      )}
      <div className={styles.cardGrid}>
        {catalogue.data.items.map((dataset) => (
          <article className={styles.card} key={dataset.manifest_hash}>
            <div className={styles.cardHeader}>
              <h2>{dataset.name}</h2>
              <span>{dataset.state}</span>
            </div>
            <dl className={styles.facts}>
              <div>
                <dt>Source and quality</dt>
                <dd>
                  {dataset.source.replaceAll("_", " ")} ·{" "}
                  {dataset.quality_tier ?? "unclassified"}
                </dd>
              </div>
              <div>
                <dt>Exchange coverage</dt>
                <dd>{dataset.exchanges.join(" and ")}</dd>
              </div>
              <div>
                <dt>Instrument scope</dt>
                <dd>
                  {dataset.instruments.length > 0
                    ? dataset.instruments.join(", ")
                    : "Not recorded for this historical manifest"}
                </dd>
              </div>
              <div>
                <dt>Recorded data</dt>
                <dd>
                  {dataset.coverage_types.length > 0
                    ? dataset.coverage_types.join(", ")
                    : "No segment-type summary is recorded"}
                </dd>
              </div>
              <div>
                <dt>Time range</dt>
                <dd>
                  {displayDate(dataset.coverage_start)} to{" "}
                  {displayDate(dataset.coverage_end)}
                </dd>
              </div>
              <div>
                <dt>Evidence health</dt>
                <dd>
                  {dataset.segment_count} segments · {dataset.known_gap_count}{" "}
                  known gaps
                </dd>
              </div>
              <div>
                <dt>Available for</dt>
                <dd>{dataset.supported_modes.join(", ")}</dd>
              </div>
              <div>
                <dt>Manifest hash</dt>
                <dd>{dataset.manifest_hash}</dd>
              </div>
            </dl>
          </article>
        ))}
      </div>
    </Page>
  );
}
