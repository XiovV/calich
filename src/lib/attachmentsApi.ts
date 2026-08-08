import { ApiError, authedFetch, errorFromResponse } from "./apiClient";
import type { Attachment } from "./event";
import { attachmentFromWire } from "./eventsApi";

function attachmentsUrl(eventId: string, attachmentId?: string): string {
  const base = `/api/events/${eventId}/attachments`;
  return attachmentId ? `${base}/${attachmentId}` : base;
}

/**
 * Uploads file against eventId, reporting upload progress as it goes.
 *
 * This is the one call in the app that uses XMLHttpRequest instead of
 * fetch — deliberately, so don't "fix" it back. `fetch` cannot report
 * upload progress in Safari or Firefox (a streaming request body is
 * Chrome-only), and MAX_ATTACHMENT_SIZE defaults to 25MB, which is tens of
 * seconds on a home upstream — an indeterminate spinner that long reads as
 * a hang and gets the modal closed mid-upload (#132, ADR-0040).
 */
function uploadAttachment(
  accessToken: string,
  eventId: string,
  file: File,
  onProgress: (fraction: number) => void,
): Promise<Attachment> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", attachmentsUrl(eventId));
    xhr.setRequestHeader("Authorization", `Bearer ${accessToken}`);

    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable) onProgress(event.loaded / event.total);
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        try {
          resolve(attachmentFromWire(JSON.parse(xhr.responseText)));
        } catch {
          reject(new Error("Failed to parse upload response."));
        }
        return;
      }

      let code = "unknown_error";
      let message = "Failed to upload file.";
      try {
        const body = JSON.parse(xhr.responseText) as {
          error?: { code?: string; message?: string };
        };
        if (body.error?.code) code = body.error.code;
        if (body.error?.message) message = body.error.message;
      } catch {
        // Body wasn't JSON — fall back to the generic message above.
      }
      reject(new ApiError(xhr.status, code, message));
    };

    xhr.onerror = () => reject(new Error("Network error while uploading."));

    const formData = new FormData();
    formData.append("file", file);
    xhr.send(formData);
  });
}

async function removeAttachment(
  accessToken: string,
  eventId: string,
  attachmentId: string,
): Promise<void> {
  const response = await authedFetch(accessToken, attachmentsUrl(eventId, attachmentId), {
    method: "DELETE",
    credentials: "include",
  });
  if (!response.ok) throw await errorFromResponse(response);
}

export const attachmentsApi = {
  upload: uploadAttachment,
  remove: removeAttachment,
  downloadUrl: (eventId: string, attachmentId: string) => attachmentsUrl(eventId, attachmentId),
};
