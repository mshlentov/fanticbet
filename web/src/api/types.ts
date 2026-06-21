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
