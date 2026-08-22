import { useState } from "react";

// Local editable state that mirrors an external value (e.g. a Zustand store
// field a save elsewhere in the same session — another form, another tab —
// can change), without clobbering an unsaved local edit. "Pristine" is
// derived by comparing the field's current value to the value it was last
// synced to, rather than tracked as a separate flag: a flag set on every
// keystroke and cleared only by this field's own save gets stuck once an
// edit is manually reverted back to the synced value without saving, since
// nothing else would ever clear it (#245 reopened).
//
// markSaved is for this field's own save completing: it forces the synced
// value even if the trimmed/normalized result differs from what's on
// screen, which the passive comparison below wouldn't catch on its own.
export function useSyncedField(externalValue: string, onExternalSync?: () => void) {
  const [value, setValue] = useState(externalValue);
  const [syncedValue, setSyncedValue] = useState(externalValue);

  if (value === syncedValue && externalValue !== syncedValue) {
    setSyncedValue(externalValue);
    setValue(externalValue);
    onExternalSync?.();
  }

  function markSaved(savedValue: string) {
    setSyncedValue(savedValue);
    setValue(savedValue);
  }

  return { value, setValue, markSaved };
}
