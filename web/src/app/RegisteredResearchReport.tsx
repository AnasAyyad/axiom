import type { APIModel } from "../api/client";
import { DataTable } from "../components/DataTable";
import styles from "./Page.module.css";

export function RegisteredResearchReport({
  report,
}: {
  readonly report: APIModel<"RegisteredResearchReport">;
}) {
  return (
    <section className={styles.card}>
      <h2>Registered generation report</h2>
      <dl className={styles.facts}>
        <div>
          <dt>Research generation</dt>
          <dd>{report.research_generation_id}</dd>
        </div>
        <div>
          <dt>Confidence</dt>
          <dd>{report.confidence_label}</dd>
        </div>
        <div>
          <dt>Platform correctness</dt>
          <dd>{report.platform_correctness}</dd>
        </div>
        <div>
          <dt>Strategy evidence</dt>
          <dd>{report.strategy_evidence}</dd>
        </div>
        <div>
          <dt>Viability</dt>
          <dd>{report.viability}</dd>
        </div>
        <div>
          <dt>Manifest hash</dt>
          <dd>{report.manifest_hash}</dd>
        </div>
        <div>
          <dt>Registered runs</dt>
          <dd>{report.run_references.length}</dd>
        </div>
      </dl>
      <DataTable
        caption="Registered benchmarks"
        rows={report.benchmarks.map((slice, index) => ({
          id: `${slice.name}-${index}`,
          ...slice,
        }))}
        columns={researchSliceColumns}
      />
      <DataTable
        caption="Registered stress scenarios"
        rows={report.stress.map((slice, index) => ({
          id: `${slice.name}-${index}`,
          ...slice,
        }))}
        columns={researchSliceColumns}
      />
      <DataTable
        caption="Registered capacity curve"
        rows={report.capacity.map((point, index) => ({
          id: `${point.notional}-${index}`,
          ...point,
        }))}
        columns={capacityColumns}
      />
      <details>
        <summary>Canonical registered manifest</summary>
        <pre className={styles.canonical}>{report.canonical_manifest}</pre>
      </details>
      <p role="note">{report.disclaimer}</p>
    </section>
  );
}

const researchSliceColumns = [
  { key: "name", label: "Scenario" },
  { key: "net_return", label: "Net return" },
  { key: "max_drawdown", label: "Maximum drawdown" },
  { key: "trades", label: "Trades" },
];

const capacityColumns = [
  { key: "notional", label: "Notional" },
  { key: "net_return", label: "Net return" },
  { key: "fill_rate", label: "Fill rate" },
];
