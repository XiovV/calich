import { authHeader, errorFromResponse } from "./apiClient";

/**
 * Downloads url's response body as filename. Auth is a Bearer header
 * (apiClient.ts), so a plain `<a href download>` would 401 — this fetches
 * with the header instead, then drives the browser's download UI through an
 * object URL and a programmatic `<a download>` click (issue #78).
 */
export async function downloadBlob(
  accessToken: string,
  url: string,
  filename: string,
): Promise<void> {
  const response = await fetch(url, {
    credentials: "include",
    headers: authHeader(accessToken),
  });
  if (!response.ok) throw await errorFromResponse(response);

  const blob = await response.blob();
  const objectUrl = URL.createObjectURL(blob);
  try {
    const anchor = document.createElement("a");
    anchor.href = objectUrl;
    anchor.download = filename;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
  } finally {
    URL.revokeObjectURL(objectUrl);
  }
}
