import { Link, useLocation, useNavigate } from "react-router-dom";

import { useAuth } from "../hooks/useAuth";
import { useTheme } from "../hooks/useTheme";
import { CoinIcon, MoonIcon, SunIcon } from "./icons";
import { NAV_ITEMS, isNavActive } from "./nav";
import { fmtCoins, initials } from "../lib/format";

// Header — фирменная шапка: логотип, навигация, баланс, переключатель темы,
// вход / аватар. Баланс и аватар видны только авторизованному.
export function Header() {
  const { status, user, balance } = useAuth();
  const { isDark, toggle } = useTheme();
  const location = useLocation();
  const navigate = useNavigate();

  const visibleNav = NAV_ITEMS.filter(
    (n) => !n.adminOnly || user?.role === "admin",
  );

  return (
    <header className="fb-header">
      <div className="fb-header-inner">
        <button type="button" className="fb-logo" onClick={() => navigate("/")}>
          <CoinIcon size={27} />
          <span>
            Fantic<span className="accent">Bet</span>
          </span>
        </button>

        <nav className="fb-nav">
          {visibleNav.map((n) => (
            <Link
              key={n.to}
              to={n.to}
              className={`fb-nav-btn${isNavActive(n, location.pathname) ? " is-active" : ""}`}
            >
              {n.label}
            </Link>
          ))}
        </nav>

        <div style={{ flex: 1 }} />

        {status === "authenticated" && balance !== null && (
          <div className="fb-balance" title="Баланс фантиков">
            <CoinIcon size={18} />
            <span>{fmtCoins(balance)}</span>
          </div>
        )}

        <button
          type="button"
          className="fb-icon-btn"
          onClick={toggle}
          title="Переключить тему"
          aria-label="Переключить тему"
        >
          {isDark ? <SunIcon /> : <MoonIcon />}
        </button>

        {status === "authenticated" && user ? (
          <button
            type="button"
            className="fb-avatar"
            title={user.display_name}
            onClick={() => navigate("/me/bets")}
          >
            {initials(user.display_name)}
          </button>
        ) : (
          <Link to="/login" className="fb-btn-outline">
            Войти
          </Link>
        )}
      </div>
    </header>
  );
}
