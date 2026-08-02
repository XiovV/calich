import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { useAuthStore } from "../lib/authStore";

interface ProtectedRouteProps {
  children: ReactNode;
}

export function ProtectedRoute({ children }: ProtectedRouteProps) {
  const session = useAuthStore((state) => state.session);

  if (!session) {
    return <Navigate to="/login" replace />;
  }

  return children;
}
