import type { APIModel } from "../api/client";
import styles from "./Page.module.css";

export type Quality = APIModel<"QualityEvidence">;

export function QualityBadge({ quality }: { readonly quality: Quality }) {
  return (
    <span
      className={styles.quality}
      data-freshness={quality.freshness}
      title={`${quality.tier} · ${quality.source}`}
    >
      {quality.freshness} · {quality.confidence}
    </span>
  );
}
