import { createContext, useContext } from "react";
import type { MeOutput } from "./api";

export type AuthStatus = "initializing" | "authenticated" | "unauthenticated";

export interface AuthContextValue {
  status: AuthStatus;
  user: MeOutput | null;
  login: (email: string, password: string) => Promise<void>;
  register: (input: { email: string; password: string; companyName: string }) => Promise<void>;
  logout: () => Promise<void>;
  refetchMe: () => Promise<void>;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
}
