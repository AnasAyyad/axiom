import { BarChart } from "echarts/charts";
import {
  AriaComponent,
  GridComponent,
  TooltipComponent,
} from "echarts/components";
import * as echarts from "echarts/core";
import { SVGRenderer } from "echarts/renderers";
import { useEffect, useRef } from "react";

import styles from "./EvidenceChart.module.css";

echarts.use([
  BarChart,
  GridComponent,
  TooltipComponent,
  AriaComponent,
  SVGRenderer,
]);

interface EvidenceChartProps {
  readonly metrics: Readonly<Record<string, string>>;
}

/** EvidenceChart is the sole project-owned ECharts adapter. Values are plotted as received. */
export function EvidenceChart({ metrics }: EvidenceChartProps) {
  const host = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!host.current) return;
    const chart = echarts.init(host.current, undefined, { renderer: "svg" });
    const entries = Object.entries(metrics).slice(0, 12);
    chart.setOption({
      animation: false,
      aria: { enabled: true, description: "Server-provided research metrics" },
      grid: { left: 140, right: 24, top: 16, bottom: 30 },
      xAxis: {
        type: "value",
        axisLabel: { color: "#91a9b2" },
        splitLine: { lineStyle: { color: "#243746" } },
      },
      yAxis: {
        type: "category",
        data: entries.map(([label]) => label),
        axisLabel: { color: "#91a9b2" },
      },
      series: [
        {
          type: "bar",
          data: entries.map(([, value]) => Number(value)),
          itemStyle: { color: "#4fd1b5" },
        },
      ],
      tooltip: {
        trigger: "axis",
        valueFormatter: (value: unknown) => String(value),
      },
    });
    const resize = () => chart.resize();
    window.addEventListener("resize", resize);
    return () => {
      window.removeEventListener("resize", resize);
      chart.dispose();
    };
  }, [metrics]);
  return (
    <div
      className={styles.chart}
      ref={host}
      role="img"
      aria-label="Server-provided research metrics chart"
    />
  );
}
