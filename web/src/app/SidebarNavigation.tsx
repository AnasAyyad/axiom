import { NavLink } from "react-router";

import type { APIModel } from "../api/client";
import { navigationFor } from "./navigation";
import styles from "./Shell.module.css";

export function SidebarNavigation({
  user,
}: {
  readonly user: APIModel<"SessionUser">;
}) {
  return (
    <nav aria-label="Axiom product navigation">
      {navigationFor(user).map((group) => (
        <section className={styles.navGroup} key={group.label}>
          <h2>{group.label}</h2>
          <div>
            {group.items.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.to === "/"}
                className={({ isActive }) =>
                  isActive ? styles.active : undefined
                }
              >
                {item.label}
              </NavLink>
            ))}
          </div>
        </section>
      ))}
    </nav>
  );
}
