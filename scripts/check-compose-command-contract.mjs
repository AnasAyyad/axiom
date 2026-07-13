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
}

process.stdout.write("Compose image-entrypoint command contract passed\n");
