import type { APIModel } from "../../api/client";

export function downloadArtifact(
  artifact: APIModel<"ExportArtifact">,
  fallback: string,
) {
  if (artifact.content === undefined) return false;
  const blob = new Blob([artifact.content], { type: artifact.content_type });
  const href = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = href;
  anchor.download = `${safeName(fallback)}.${artifact.format}`;
  anchor.click();
  URL.revokeObjectURL(href);
  return true;
}

function safeName(value: string) {
  return value.replace(/[^a-zA-Z0-9._-]+/g, "-").slice(0, 96);
}
