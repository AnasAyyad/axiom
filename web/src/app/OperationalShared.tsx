import type { ReactNode } from "react";

import { HelpPopover } from "../components/HelpPopover";
import styles from "./Page.module.css";

export function Page({
  title,
  eyebrow,
  description,
  children,
}: {
  readonly title: string;
  readonly eyebrow: string;
  readonly description: string;
  readonly children: ReactNode;
}) {
  return (
    <section className={styles.page}>
      <header className={styles.header}>
        <div>
          <span className={styles.eyebrow}>{eyebrow}</span>
          <h1>{title}</h1>
          <p>{description}</p>
        </div>
        <div className={styles.pageHelp}>
          <HelpPopover label="About this page">
            <p>
              This page shows the owner-facing information described above. It
              uses the most recently retrieved server-authoritative projection;
              an empty, stale, or blocked section explains what is missing and
              the next safe action. Values are operational evidence, not proof
              of strategy profitability.
            </p>
          </HelpPopover>
        </div>
      </header>
      {children}
    </section>
  );
}
export function Facts({
  title,
  values,
}: {
  readonly title: string;
  readonly values: Readonly<Record<string, string>>;
}) {
  return (
    <article className={styles.card}>
      <h2>{title}</h2>
      <dl className={styles.facts}>
        {Object.entries(values).map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </article>
  );
}
