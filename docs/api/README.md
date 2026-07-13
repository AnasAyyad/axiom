# API contracts

`api/openapi.yaml` is the OpenAPI root and authoritative HTTP shape. A1 defines
only health, version, build-information, and system-status reads. A11 expands the
contract to the complete authenticated research workflow.

Run `make contracts` to generate:

- `internal/api/generated/types.gen.go` with pinned `oapi-codegen`; and
- `web/src/api/generated/schema.ts` with the project-owned deterministic A1
  schema generator.

`make contracts-check` regenerates into a temporary directory and fails on any
drift. Generated files are never hand-edited. REST snapshots remain
authoritative; resumable SSE is implemented in A11.
