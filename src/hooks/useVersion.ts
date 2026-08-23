import { useEffect, useState } from "react";
import { getVersion } from "../lib/versionApi";

// The instance's build label for display (#256, ADR-0072), or null until it
// arrives — and null forever if it never does.
//
// Deliberately not a store: nothing writes this, and one component reads
// it, so the state machinery the shell/events/calendars stores exist for
// (ADR-0001, ADR-0003, ADR-0005) would be ceremony with no reason. The
// single request is shared by versionApi's own module-level memo.
export function useVersion(): string | null {
  const [version, setVersion] = useState<string | null>(null);

  useEffect(() => {
    let active = true;

    getVersion()
      .then((label) => {
        if (active) setVersion(label);
      })
      .catch(() => {
        // Swallowed on purpose. The badge is ambient information nobody is
        // blocked on, so a failure shows nothing rather than a placeholder,
        // an error, or a toast.
      });

    return () => {
      active = false;
    };
  }, []);

  return version;
}
