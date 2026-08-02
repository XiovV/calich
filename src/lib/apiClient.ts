export class ApiError extends Error {
  code: string;
  status: number;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

export async function errorFromResponse(response: Response): Promise<ApiError> {
  let code = "unknown_error";
  let message = "Something went wrong.";
  try {
    const body = (await response.json()) as { error?: { code?: string; message?: string } };
    if (body.error?.code) code = body.error.code;
    if (body.error?.message) message = body.error.message;
  } catch {
    // Body wasn't JSON (or was empty) — fall back to the generic message above.
  }
  return new ApiError(response.status, code, message);
}

export function authHeader(accessToken: string): HeadersInit {
  return { Authorization: `Bearer ${accessToken}` };
}
