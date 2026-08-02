import { useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Field } from "@base-ui/react/field";
import type { Calendar } from "../../lib/calendar";
import { getNextUnusedColor } from "../../lib/calendarColors";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useShellStore } from "../../lib/shellStore";
import { ColorSwatchPicker } from "./ColorSwatchPicker";

type CalendarModalProps =
  | { mode: "create"; onClose: () => void }
  | { mode: "edit"; calendar: Calendar; onClose: () => void };

export function CalendarModal(props: CalendarModalProps) {
  const { mode, onClose } = props;

  const calendars = useCalendarsStore((state) => state.calendars);
  const addCalendar = useCalendarsStore((state) => state.addCalendar);
  const updateCalendar = useCalendarsStore((state) => state.updateCalendar);
  const toggleCalendarChecked = useShellStore(
    (state) => state.toggleCalendarChecked,
  );

  const [name, setName] = useState(mode === "edit" ? props.calendar.name : "");
  const [color, setColor] = useState(() =>
    mode === "edit"
      ? props.calendar.color
      : getNextUnusedColor(calendars.map((calendar) => calendar.color)),
  );

  const canSave = name.trim() !== "";

  function handleSave() {
    if (!canSave) return;

    if (mode === "edit") {
      updateCalendar(props.calendar.id, { name: name.trim(), color });
    } else {
      const id = crypto.randomUUID();
      addCalendar({ id, name: name.trim(), color });
      toggleCalendarChecked(id);
    }
    onClose();
  }

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 bg-ink/20" />
        <Dialog.Popup className="fixed top-1/2 left-1/2 w-80 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
          <Dialog.Title className="text-heading font-medium text-ink">
            {mode === "edit" ? "Edit calendar" : "New calendar"}
          </Dialog.Title>

          <Field.Root className="mt-4">
            <Field.Label className="block text-label-sm text-ink-muted">
              Name
            </Field.Label>
            <Field.Control
              value={name}
              onChange={(event) => setName(event.target.value)}
              placeholder="Calendar name"
              className="mt-1 w-full rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink"
            />
          </Field.Root>

          <div className="mt-4">
            <p className="text-label-sm text-ink-muted">Color</p>
            <div className="mt-1">
              <ColorSwatchPicker value={color} onValueChange={setColor} />
            </div>
          </div>

          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close className="rounded-shell-sm border border-border px-3 py-1.5 text-body text-ink hover:bg-surface-hover">
              Cancel
            </Dialog.Close>
            <button
              type="button"
              onClick={handleSave}
              disabled={!canSave}
              className="rounded-shell-sm bg-accent px-3 py-1.5 text-body text-ink-inverse hover:bg-accent-hover disabled:opacity-50"
            >
              Save
            </button>
          </div>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
