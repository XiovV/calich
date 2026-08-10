import { authedFetch, errorFromResponse } from "./apiClient";

// UserSummary is one entry in the User directory (#113) — enough to pick a
// Share recipient, and deliberately nothing more (no admin/disabled status,
// unlike the Admin-only account listing).
export interface UserSummary {
  id: number;
  name: string;
  email: string;
}

export const usersApi = {
  // directory returns every enabled User besides the caller, open to any
  // authenticated caller (#113).
  async directory(accessToken: string): Promise<UserSummary[]> {
    const response = await authedFetch(accessToken, "/api/users/", {
      credentials: "include",
    });
    if (!response.ok) throw await errorFromResponse(response);

    return (await response.json()) as UserSummary[];
  },
};
