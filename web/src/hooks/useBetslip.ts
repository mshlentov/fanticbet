import { useContext } from "react";

import { BetslipContext } from "../context/BetslipContext";
import type { BetslipContextValue } from "../context/BetslipContext";

export function useBetslip(): BetslipContextValue {
  const ctx = useContext(BetslipContext);
  if (!ctx) {
    throw new Error("useBetslip должен использоваться внутри <BetslipProvider>");
  }
  return ctx;
}
