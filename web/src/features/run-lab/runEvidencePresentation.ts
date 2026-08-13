export type EvidenceTab =
  | "timeline"
  | "decisions"
  | "orders"
  | "portfolio"
  | "risk"
  | "data"
  | "evidence"
  | "overview";

export function readableMachineValue(value: unknown) {
  return typeof value === "string"
    ? value.replaceAll("_", " ").replaceAll("-", " ")
    : "recorded";
}

export function exchangeLabel(value: unknown) {
  if (value === "binance") return "Binance Spot Testnet";
  if (value === "bybit") return "Bybit Demo";
  return "Recorded venue";
}
