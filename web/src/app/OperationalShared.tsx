import type { ReactNode } from "react";

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
