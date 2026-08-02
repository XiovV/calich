import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { useAuthStore } from "../lib/authStore";
import { ChangePasswordScreen } from "./ChangePasswordScreen";

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

  if (status === "must-change-password") {
    return <ChangePasswordScreen />;
  }

  return children;
}
