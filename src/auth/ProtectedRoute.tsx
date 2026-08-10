import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { useAuthStore } from "../lib/authStore";
import { ChangePasswordScreen } from "./ChangePasswordScreen";
import { ReactivateAccountScreen } from "./ReactivateAccountScreen";

interface ProtectedRouteProps {
  children: ReactNode;
}

export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const status = useAuthStore((state) => state.status);

  if (status === "loading") {
    return null;
  }

  if (status === "unauthenticated") {
    return <Navigate to="/login" replace />;
  }

  // A fresh instance with zero accounts has nothing to sign in to (#169,
  // ADR-0047) — go straight to Register instead.
  if (status === "needs-setup") {
    return <Navigate to="/register" replace />;
  }

  if (status === "must-change-password") {
    return <ChangePasswordScreen />;
  }

  if (status === "account-disabled") {
    return <ReactivateAccountScreen />;
  }

  return children;
}
