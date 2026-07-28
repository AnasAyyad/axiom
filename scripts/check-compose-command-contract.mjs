import { readFileSync } from "node:fs";

const config = JSON.parse(readFileSync(0, "utf8"));
const expectedCommands = new Map([
  ["migrate", ["admin", "migrate"]],
  ["api", ["api"]],
  ["engine-shadow", ["trader", "--mode=shadow"]],
  ["recorder", ["recorder"]],
  ["backtest-worker", ["worker"]],
  ["binance-testnet-egress", ["egress-proxy", "--exchange=binance"]],
  ["bybit-demo-egress", ["egress-proxy", "--exchange=bybit"]],
]);

for (const [serviceName, expected] of expectedCommands) {
  const service = config.services?.[serviceName];
  if (!service) {
    throw new Error(`missing application service: ${serviceName}`);
  }
  if (service.entrypoint !== undefined && service.entrypoint !== null) {
    throw new Error(`${serviceName} must use the image entrypoint`);
  }
  if (JSON.stringify(service.command) !== JSON.stringify(expected)) {
    throw new Error(
      `${serviceName} command is ${JSON.stringify(service.command)}, want ${JSON.stringify(expected)}`,
    );
  }
  if (service.user !== "10001:70") {
    throw new Error(`${serviceName} must run as the pinned non-root identity`);
  }
  if (service.read_only !== true) {
    throw new Error(`${serviceName} must use a read-only root filesystem`);
  }
  if (!service.cap_drop?.includes("ALL")) {
    throw new Error(`${serviceName} must drop all Linux capabilities`);
  }
  if (!service.security_opt?.includes("no-new-privileges:true")) {
    throw new Error(`${serviceName} must set no-new-privileges`);
  }
  if (!service.pids_limit || !service.mem_limit) {
    throw new Error(`${serviceName} must define PID and memory limits`);
  }
}

for (const [serviceName, internalNetwork, externalNetwork] of [
  ["binance-testnet-egress", "binance_engine", "binance_proxy_egress"],
  ["bybit-demo-egress", "bybit_engine", "bybit_proxy_egress"],
]) {
  const service = config.services[serviceName];
  const networks = Object.keys(service.networks ?? {});
  if (
    networks.length !== 2 ||
    !networks.includes(internalNetwork) ||
    !networks.includes(externalNetwork)
  ) {
    throw new Error(`${serviceName} does not have its exact isolated networks`);
  }
  if ((service.secrets ?? []).length !== 0) {
    throw new Error(`${serviceName} must not receive credentials`);
  }
}

for (const serviceName of [
  "api",
  "engine-shadow",
  "recorder",
  "backtest-worker",
]) {
  const secrets = (config.services[serviceName]?.secrets ?? []).map(
    (item) => item.source ?? item,
  );
  if (
    secrets.some(
      (name) =>
        String(name).startsWith("binance_testnet_api_") ||
        String(name).startsWith("bybit_demo_api_"),
    )
  ) {
    throw new Error(`${serviceName} receives an exchange credential`);
  }
}

const postgresHealth = config.services?.postgres?.healthcheck?.test;
if (
  !Array.isArray(postgresHealth) ||
  postgresHealth[0] !== "CMD-SHELL" ||
  !postgresHealth[1]?.includes("/proc/1/comm") ||
  !postgresHealth[1]?.includes("pg_isready")
) {
  throw new Error(
    "postgres health must wait for the final PID 1 server after initialization",
  );
}

process.stdout.write("Compose image-entrypoint command contract passed\n");
