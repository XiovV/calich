import { ApiError, authHeader, errorFromResponse } from "./apiClient";

export { ApiError };

export interface User {
  id: number;
  username: string;
  mustChangePassword: boolean;
}

export interface LoginResult {
  accessToken: string;
  mustChangePassword: boolean;
}

export const authApi = {
  async login(username: string, password: string): Promise<LoginResult> {
    const response = await fetch("/api/auth/login", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { access_token: string; must_change_password: boolean };
    return { accessToken: body.access_token, mustChangePassword: body.must_change_password };
  },

  // Relies on the httpOnly refresh_token cookie the browser sends automatically —
  // there is nothing to pass in.
  async refresh(): Promise<{ accessToken: string }> {
    const response = await fetch("/api/auth/refresh", {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { access_token: string };
    return { accessToken: body.access_token };
  },

  async logout(): Promise<void> {
    const response = await fetch("/api/auth/logout", {
      method: "POST",
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);
  },

  async me(accessToken: string): Promise<User> {
    const response = await fetch("/api/auth/me", {
      credentials: "include",
      headers: authHeader(accessToken),
    });
    if (!response.ok) throw await errorFromResponse(response);

    const body = (await response.json()) as { id: number; username: string; must_change_password: boolean };
    return { id: body.id, username: body.username, mustChangePassword: body.must_change_password };
  },

  async changePassword(accessToken: string, currentPassword: string, newPassword: string): Promise<void> {
    const response = await fetch("/api/auth/change-password", {
      method: "POST",
      credentials: "include",
      headers: { "Content-Type": "application/json", ...authHeader(accessToken) },
      body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
    });
    if (!response.ok) throw await errorFromResponse(response);
  },
};
