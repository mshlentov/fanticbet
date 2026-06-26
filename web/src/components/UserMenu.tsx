import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { useAuth } from "../hooks/useAuth";
import { initials } from "../lib/format";

type UserMenuProps = {
  displayName: string;
  // placement — куда раскрывается меню относительно аватара:
  // "down" — вниз (шапка), "up" — вверх (мобильная навигация снизу экрана).
  placement?: "down" | "up";
};

// UserMenu — аватар с выпадающим меню «Профиль» / «Выйти».
// Единая reusable-реализация для шапки и мобильной навигации:
// закрывается по клику вне меню и по Esc, после выхода редиректит на главную.
export function UserMenu({ displayName, placement = "down" }: UserMenuProps) {
  const navigate = useNavigate();
  const { logout } = useAuth();
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  // Закрытие дропдауна по клику вне и по нажатию Esc — только пока он открыт.
  useEffect(() => {
    if (!open) return;
    const onPointerDown = (e: PointerEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("pointerdown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const goProfile = () => {
    setOpen(false);
    navigate("/me/bets");
  };

  // doLogout — вызывает готовый logout из useAuth и уводит на главную.
  const doLogout = async () => {
    setOpen(false);
    await logout();
    navigate("/");
  };

  return (
    <div className="fb-usermenu" ref={rootRef}>
      <button
        type="button"
        className="fb-avatar"
        title={displayName}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {initials(displayName)}
      </button>

      {open && (
        <div className={`fb-menu fb-menu-${placement}`} role="menu">
          <button
            type="button"
            className="fb-menu-item"
            role="menuitem"
            onClick={goProfile}
          >
            Профиль
          </button>
          <button
            type="button"
            className="fb-menu-item"
            role="menuitem"
            onClick={doLogout}
          >
            Выйти
          </button>
        </div>
      )}
    </div>
  );
}
