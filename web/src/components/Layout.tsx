import { Outlet } from "react-router-dom";

import { Header } from "./Header";
import { MobileNav } from "./MobileNav";
import { Betslip, SlipFab } from "./Betslip";

// Layout — общий каркас: шапка, контент (Outlet), купон ставок, плавающая
// кнопка купона и мобильная навигация. Тосты рендерит ToastProvider.
export function Layout() {
  return (
    <div className="fb-page">
      <Header />
      <main className="fb-main">
        <Outlet />
      </main>
      <Betslip />
      <SlipFab />
      <MobileNav />
    </div>
  );
}
