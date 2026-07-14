import { readFileSync } from "node:fs";

const config = JSON.parse(readFileSync(0, "utf8"));
const expectedCommands = new Map([
  ["migrate", ["admin", "migrate"]],
  ["api", ["api"]],
  ["engine-shadow", ["trader", "--mode=shadow"]],
  ["recorder", ["recorder"]],
  ["backtest-worker", ["worker"]],
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
