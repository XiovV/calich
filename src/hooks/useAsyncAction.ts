import { useState } from "react";
import { errorMessage } from "../lib/errorMessage";

// The in-flight state machine shared by the app's forms and toggles: refuse
// re-entry while a previous run is still going, clear the last error, run the
// action, and surface a failure as a message.
//
// Deliberately owns nothing else. Whatever varies between call sites — a
// preventDefault, a validity guard, a "Saved." flag, a per-row busy id — stays
// in the caller's handler, so this stays a state machine rather than becoming
// a form framework.
export function useAsyncAction() {
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function run(action: () => Promise<void>) {
    if (isSubmitting) return;

    setIsSubmitting(true);
    setError(null);
    try {
      await action();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setIsSubmitting(false);
    }
  }

  return { isSubmitting, error, setError, run };
}
