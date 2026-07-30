export function sandboxEnvironmentName(value: {
  readonly exchange: "binance" | "bybit";
  readonly environment?: "spot_testnet" | "demo";
}) {
  return value.exchange === "binance" ? "Binance Spot Testnet" : "Bybit Demo";
}

export function yesNo(value: boolean) {
  return value ? "yes" : "no";
}
