import { Navigate, Outlet, useLocation } from "react-router-dom";

import { useAuth } from "../hooks/useAuth";
import { LoadingState } from "./states";

// ProtectedRoute — оборачивает маршруты, требующие авторизации. Пока идёт
// начальная проверка сессии — спиннер; гостя уводит на /login (с запоминанием
// исходного пути). adminOnly — дополнительно требует роль admin.
export function ProtectedRoute({ adminOnly = false }: { adminOnly?: boolean }) {
  const { status, user } = useAuth();
  const location = useLocation();

  if (status === "loading") {
    return <LoadingState label="Проверяем сессию…" />;
  }

  if (status === "guest") {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  if (adminOnly && user?.role !== "admin") {
    return <Navigate to="/" replace />;
  }

  return <Outlet />;
}
