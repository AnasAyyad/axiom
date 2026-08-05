import type { APIModel } from "../../api/client";

export function hasAccess(
  user: APIModel<"SessionUser">,
  _required: readonly string[],
) {
  return user.id.length > 0;
}

export function roleLabel(user: APIModel<"SessionUser">) {
  return user.email ? "Owner" : "Owner session";
}
