import type { APIModel } from "../../api/client";

export function hasAccess(
  user: APIModel<"SessionUser">,
  required: readonly string[],
) {
  if (user.roles.includes("owner")) return true;
  const permissions = new Set(user.permissions);
  return required.some((permission) => permissions.has(permission));
}

export function roleLabel(user: APIModel<"SessionUser">) {
  if (user.roles.includes("owner")) return "Owner / Admin";
  if (user.roles.includes("operator")) return "Operator";
  if (user.roles.includes("researcher")) return "Researcher";
  if (user.roles.includes("auditor")) return "Auditor";
  return "Read-only";
}
