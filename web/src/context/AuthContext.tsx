import { createContext, useCallback, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";

import { setOnAuthFailure } from "../api/client";
import * as authApi from "../api/auth";
import type { MeResponse, User } from "../api/types";

// Статус сессии: loading — идёт начальная проверка (silent refresh + /me).
export type AuthStatus = "loading" | "authenticated" | "guest";

export type AuthContextValue = {
  status: AuthStatus;
  user: User | null;
  balance: number | null;
  login: (email: string, password: string) => Promise<void>;
  register: (
    email: string,
    password: string,
    displayName: string,
  ) => Promise<void>;
  logout: () => Promise<void>;
  // refresh — перечитать профиль/баланс (например, после размещения ставки).
  refresh: () => Promise<void>;
};

// eslint-disable-next-line react-refresh/only-export-components
export const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<AuthStatus>("loading");
  const [me, setMe] = useState<MeResponse | null>(null);

  // applyMe / clear — единые точки смены состояния авторизации.
  const applyMe = useCallback((data: MeResponse) => {
    setMe(data);
    setStatus("authenticated");
  }, []);

  const clear = useCallback(() => {
    setMe(null);
    setStatus("guest");
  }, []);

  const refresh = useCallback(async () => {
    const data = await authApi.getMe();
    applyMe(data);
  }, [applyMe]);

  // Bootstrap: при монтировании регистрируем обработчик протухшей сессии и
  // пытаемся получить профиль. getMe сам инициирует silent-refresh на 401.
  useEffect(() => {
    setOnAuthFailure(clear);

    let cancelled = false;
    authApi
      .getMe()
      .then((data) => {
        if (!cancelled) applyMe(data);
      })
      .catch(() => {
        if (!cancelled) clear();
      });

    return () => {
      cancelled = true;
      setOnAuthFailure(null);
    };
  }, [applyMe, clear]);

  const login = useCallback(
    async (email: string, password: string) => {
      await authApi.login(email, password);
      await refresh();
    },
    [refresh],
  );

  const register = useCallback(
    async (email: string, password: string, displayName: string) => {
      await authApi.register(email, password, displayName);
      await refresh();
    },
    [refresh],
  );

  const logout = useCallback(async () => {
    await authApi.logout();
    clear();
  }, [clear]);

  const value = useMemo<AuthContextValue>(
    () => ({
      status,
      user: me?.user ?? null,
      balance: me?.balance ?? null,
      login,
      register,
      logout,
      refresh,
    }),
    [status, me, login, register, logout, refresh],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
