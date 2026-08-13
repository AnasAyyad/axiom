export function stringAttribute(
  attributes: Readonly<Record<string, unknown>>,
  key: string,
  fallback = "Unavailable",
) {
  const value = attributes[key];
  return typeof value === "string" ? value : fallback;
}

export function stringListAttribute(
  attributes: Readonly<Record<string, unknown>>,
  key: string,
) {
  const value = attributes[key];
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string")
    : [];
}

export function strategyPurpose(family: string) {
  const purposes: Record<string, string> = {
    trend:
      "Research completed-candle directional persistence under centralized allocation and risk controls.",
    mean_reversion:
      "Research bounded return-to-baseline behavior using coherent historical and replay evidence.",
    triangular:
      "Evaluate closed-cycle spot conversion opportunities through deterministic simulation only.",
    triangular_arbitrage:
      "Evaluate closed-cycle spot conversion opportunities through deterministic simulation only.",
    cross_exchange:
      "Compare coherent public books and virtual inventory across approved exchanges without production execution.",
  };
  return (
    purposes[family] ??
    "Evaluate a registered strategy version through approved research and virtual execution modes."
  );
}
