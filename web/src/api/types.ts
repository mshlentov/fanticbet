// Типы ответов API. Зеркалят DTO бэкенда (internal/handler/*.go).
// Деньги — целые (int64 на бэке → number здесь). Коэффициенты бэкенд
// сериализует как JSON-число (shopspring/decimal), поэтому odds — number.

export type TokenResponse = {
  access_token: string;
  token_type: string;
  expires_in: number;
};

export type Role = "user" | "admin";

export type User = {
  id: number;
  email: string | null;
  display_name: string;
  avatar_url: string | null;
  role: Role;
  created_at: string;
  last_login_at: string | null;
};

// Ответ GET /me и PATCH /me: профиль + баланс одним объектом.
export type MeResponse = {
  user: User;
  balance: number;
};

export type EventStatus = "upcoming" | "live" | "settled" | "cancelled";
export type MarketType = "ML" | "TOTALS" | "CUSTOM";
export type MarketStatus = "open" | "suspended" | "settled" | "void";
export type OutcomeResult = "won" | "lost" | "void" | null;

export type Outcome = {
  id: number;
  code: string;
  label: string;
  odds: number;
  result: OutcomeResult;
};

export type Market = {
  id: number;
  type: MarketType;
  line: number | null;
  question: string | null;
  status: MarketStatus;
  outcomes: Outcome[];
};

export type Event = {
  id: number;
  source: string;
  sport_slug: string;
  league_name: string | null;
  title: string;
  home: string | null;
  away: string | null;
  starts_at: string;
  status: EventStatus;
  markets: Market[];
};

// Пагинированная лента событий (GET /events).
export type EventsPage = {
  page: number;
  items: Event[];
};

export type SportsResponse = {
  sports: string[];
};

export type BetStatus = "pending" | "won" | "lost" | "void";

export type Bet = {
  id: number;
  outcome_id: number;
  event_id: number;
  stake: number;
  odds: number;
  potential_payout: number;
  status: BetStatus;
  settled_at: string | null;
  created_at: string;
};

export type PlaceBetResponse = {
  bet: Bet;
  balance: number;
};

export type BetsPage = {
  page: number;
  items: Bet[];
};

export type TransactionType =
  | "signup_bonus"
  | "bet_stake"
  | "bet_payout"
  | "bet_refund"
  | "admin_adjust";

export type WalletTransaction = {
  id: number;
  amount: number;
  type: TransactionType;
  bet_id: number | null;
  balance_after: number;
  created_at: string;
};

export type TransactionsPage = {
  page: number;
  items: WalletTransaction[];
};

// --- Социальная часть (M5): профиль со статистикой и лидерборд ---

// UserStats — агрегированная статистика по ставкам. Счётчики и суммы — целые;
// win_rate/roi — доли (0..1; roi может быть > 1 при прибыли больше оборота).
export type UserStats = {
  total_bets: number;
  won_bets: number;
  lost_bets: number;
  void_bets: number;
  pending_bets: number;
  staked: number;
  profit: number;
  win_rate: number;
  roi: number;
};

// PublicProfile — ответ GET /users/:id: публичные поля + статистика.
export type PublicProfile = {
  id: number;
  display_name: string;
  avatar_url: string | null;
  created_at: string;
  stats: UserStats;
};

// Ответ GET /users/:id/bets — страница публичной истории ставок.
export type UserBetsPage = {
  page: number;
  items: Bet[];
};

export type LeaderboardPeriod = "week" | "month" | "all";
export type LeaderboardMetric = "profit" | "roi";

// LeaderboardRow — строка таблицы лидеров: публичное имя/аватар + метрики.
export type LeaderboardRow = {
  user_id: number;
  display_name: string;
  avatar_url: string | null;
  total_bets: number;
  won_bets: number;
  staked: number;
  profit: number;
  roi: number;
};

export type LeaderboardPage = {
  page: number;
  items: LeaderboardRow[];
};
