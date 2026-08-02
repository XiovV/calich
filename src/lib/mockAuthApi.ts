const TEST_USER = {
  email: "demo@example.com",
  password: "password123",
};

const MOCK_DELAY_MS = 400;

export interface Session {
  user: { email: string };
  accessToken: string;
  refreshToken: string;
}

export class InvalidCredentialsError extends Error {
  constructor() {
    super("Invalid email or password.");
    this.name = "InvalidCredentialsError";
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export const mockAuthApi = {
  async login(email: string, password: string): Promise<Session> {
    await delay(MOCK_DELAY_MS);

    if (email !== TEST_USER.email || password !== TEST_USER.password) {
      throw new InvalidCredentialsError();
    }

    return {
      user: { email: TEST_USER.email },
      accessToken: `mock-access-${crypto.randomUUID()}`,
      refreshToken: `mock-refresh-${crypto.randomUUID()}`,
    };
  },

  // The refresh token isn't used yet — this mock has no real session to check
  // it against — but it's part of the call's real shape (a real backend would
  // need it) so it stays in the signature rather than being dropped.
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  async refresh(_refreshToken: string): Promise<{ accessToken: string }> {
    await delay(MOCK_DELAY_MS);
    return { accessToken: `mock-access-${crypto.randomUUID()}` };
  },
};
