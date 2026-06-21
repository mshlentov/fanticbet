import { Link, useLocation } from "react-router-dom";

import { useAuth } from "../hooks/useAuth";
import { NAV_ITEMS, isNavActive } from "./nav";

// MobileNav — нижняя навигация для узких экранов (видна через CSS @media).
export function MobileNav() {
  const { user } = useAuth();
  const location = useLocation();
  const visibleNav = NAV_ITEMS.filter(
    (n) => !n.adminOnly || user?.role === "admin",
  );

  return (
    <nav className="fb-mobile-nav">
      {visibleNav.map((n) => (
        <Link
          key={n.to}
          to={n.to}
          className={isNavActive(n, location.pathname) ? "is-active" : ""}
          style={{
            flex: 1,
            padding: "9px 4px",
            borderRadius: 10,
            textAlign: "center",
            fontSize: 12,
            fontWeight: 700,
          }}
        >
          {n.label}
        </Link>
      ))}
    </nav>
  );
}
