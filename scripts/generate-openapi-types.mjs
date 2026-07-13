import fs from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";

const require = createRequire(new URL("../web/package.json", import.meta.url));
const { parse } = require("yaml");

const [sourcePath, outputPath] = process.argv.slice(2);
if (!sourcePath || !outputPath) {
  console.error(
    "usage: node scripts/generate-openapi-types.mjs <openapi.yaml> <output.ts>",
  );
  process.exit(2);
}

const document = parse(fs.readFileSync(sourcePath, "utf8"));
const schemas = document?.components?.schemas;
if (typeof schemas !== "object" || schemas === null || Array.isArray(schemas)) {
  throw new Error("openapi_components_schemas_missing");
}

function typeFor(schema) {
  if (schema.$ref) {
    const name = schema.$ref.split("/").at(-1);
    return `components["schemas"][${JSON.stringify(name)}]`;
  }
  if (Array.isArray(schema.enum)) {
    return schema.enum.map((value) => JSON.stringify(value)).join(" | ");
  }
  if (schema.type === "array") {
    return `Array<${typeFor(schema.items ?? {})}>`;
  }
  if (schema.type === "boolean") return "boolean";
  if (schema.type === "integer" || schema.type === "number") return "number";
  if (schema.type === "string") return "string";
  if (schema.type === "object") return objectType(schema);
  throw new Error("unsupported_openapi_schema");
}

function objectType(schema) {
  const properties = schema.properties ?? {};
  const required = new Set(schema.required ?? []);
  const lines = Object.keys(properties)
    .sort()
    .map((name) => {
      const optional = required.has(name) ? "" : "?";
      return `      ${JSON.stringify(name)}${optional}: ${typeFor(properties[name])};`;
    });
  return `{\n${lines.join("\n")}\n    }`;
}

const lines = [
  "// Code generated from api/openapi.yaml by scripts/generate-openapi-types.mjs.",
  "// DO NOT EDIT.",
  "",
  "export interface components {",
  "  schemas: {",
];
for (const name of Object.keys(schemas).sort()) {
  lines.push(`    ${JSON.stringify(name)}: ${typeFor(schemas[name])};`);
}
lines.push("  };", "}", "");

fs.mkdirSync(path.dirname(outputPath), { recursive: true });
fs.writeFileSync(outputPath, lines.join("\n"));
