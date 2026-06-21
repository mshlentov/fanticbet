import { Routes, Route } from "react-router-dom";

import { Layout } from "./components/Layout";
import { ProtectedRoute } from "./components/ProtectedRoute";
import { EventsPage } from "./pages/EventsPage";
import { EventDetailPage } from "./pages/EventDetailPage";
import { LoginPage } from "./pages/LoginPage";
import { RegisterPage } from "./pages/RegisterPage";
import { MyBetsPage } from "./pages/MyBetsPage";
import { UserProfilePage } from "./pages/UserProfilePage";
import { LeaderboardPage } from "./pages/LeaderboardPage";
import { AdminPage } from "./pages/AdminPage";
import { NotFoundPage } from "./pages/NotFoundPage";

// App — карта маршрутов SPA. Публичные страницы и страницы за авторизацией
// (ProtectedRoute) лежат под общим Layout (шапка + контейнер). Часть страниц
// пока заглушки под вехи M5/M6.
export default function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        {/* Публичные */}
        <Route index element={<EventsPage />} />
        <Route path="events/:id" element={<EventDetailPage />} />
        <Route path="users/:id" element={<UserProfilePage />} />
        <Route path="leaderboard" element={<LeaderboardPage />} />
        <Route path="login" element={<LoginPage />} />
        <Route path="register" element={<RegisterPage />} />

        {/* Требуют авторизации */}
        <Route element={<ProtectedRoute />}>
          <Route path="me/bets" element={<MyBetsPage />} />
        </Route>

        {/* Только админ */}
        <Route element={<ProtectedRoute adminOnly />}>
          <Route path="admin" element={<AdminPage />} />
        </Route>

        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  );
}
