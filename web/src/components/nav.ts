// Описание пунктов навигации — общее для десктоп-шапки и мобильного меню.
export type NavItem = {
  label: string;
  to: string;
  // matches — какие префиксы пути считаются активными для этого пункта.
  matches: string[];
  adminOnly?: boolean;
};

export const NAV_ITEMS: NavItem[] = [
  { label: "Лента", to: "/", matches: ["/", "/events"] },
  { label: "Мои ставки", to: "/me/bets", matches: ["/me/bets"] },
  { label: "Лидерборд", to: "/leaderboard", matches: ["/leaderboard", "/users"] },
  { label: "Админ", to: "/admin", matches: ["/admin"], adminOnly: true },
];

// isNavActive — активен ли пункт для текущего пути.
export function isNavActive(item: NavItem, pathname: string): boolean {
  if (item.to === "/") {
    return pathname === "/" || pathname.startsWith("/events");
  }
  return item.matches.some((m) => pathname === m || pathname.startsWith(m + "/"));
}
