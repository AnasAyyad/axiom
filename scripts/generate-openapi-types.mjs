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
  if (Array.isArray(schema.allOf)) {
    return schema.allOf.map(typeFor).join(" & ");
  }
  if (Array.isArray(schema.oneOf)) {
    return schema.oneOf.map(typeFor).join(" | ");
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
  if (Object.keys(properties).length === 0 && schema.additionalProperties) {
    const value =
      schema.additionalProperties === true
        ? "unknown"
        : typeFor(schema.additionalProperties);
    return `Record<string, ${value}>`;
  }
  const required = new Set(schema.required ?? []);
  const lines = Object.keys(properties)
    .sort()
    .map((name) => {
      const optional = required.has(name) ? "" : "?";
      return `      ${JSON.stringify(name)}${optional}: ${typeFor(properties[name])};`;
    });
  return `{\n${lines.join("\n")}\n    }`;
}

function resolve(reference) {
  if (!reference?.$ref) return reference;
  const parts = reference.$ref.replace(/^#\//, "").split("/");
  return parts.reduce((value, part) => value?.[part], document);
}

function contentType(item) {
  const resolved = resolve(item);
  const content = resolved?.content;
  if (!content || typeof content !== "object") return "never";
  const media = content["application/json"] ?? Object.values(content)[0];
  return media?.schema ? typeFor(media.schema) : "never";
}

function operationType(pathItem, operation) {
  const parameters = [
    ...(pathItem.parameters ?? []),
    ...(operation.parameters ?? []),
  ]
    .map(resolve)
    .filter(Boolean);
  const grouped = { path: [], query: [], header: [], cookie: [] };
  for (const parameter of parameters) {
    const optional = parameter.required ? "" : "?";
    grouped[parameter.in]?.push(
      `${JSON.stringify(parameter.name)}${optional}: ${typeFor(parameter.schema ?? { type: "string" })};`,
    );
  }
  const lines = [];
  for (const location of Object.keys(grouped)) {
    if (grouped[location].length > 0) {
      lines.push(`${location}: { ${grouped[location].join(" ")} };`);
    }
  }
  if (operation.requestBody) {
    lines.push(`requestBody: ${contentType(operation.requestBody)};`);
  }
  const responses = Object.entries(operation.responses ?? {}).map(
    ([status, response]) =>
      `${JSON.stringify(status)}: ${contentType(response)};`,
  );
  lines.push(`responses: { ${responses.join(" ")} };`);
  return `{ ${lines.join(" ")} }`;
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

lines.push("export interface operations {");
for (const [pathName, pathItem] of Object.entries(document.paths ?? {})) {
  for (const method of ["get", "post", "put", "patch", "delete"]) {
    const operation = pathItem?.[method];
    if (operation?.operationId) {
      lines.push(
        `  ${JSON.stringify(operation.operationId)}: ${operationType(pathItem, operation)};`,
      );
    }
  }
}
lines.push("}", "");

fs.mkdirSync(path.dirname(outputPath), { recursive: true });
fs.writeFileSync(outputPath, lines.join("\n"));
