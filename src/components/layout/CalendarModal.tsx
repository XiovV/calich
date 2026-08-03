import { useState, type KeyboardEvent } from "react";
import { Dialog } from "@base-ui/react/dialog";
import type { Calendar } from "../../lib/calendar";
import { getNextUnusedColor } from "../../lib/calendarColors";
import { useCalendarsStore } from "../../lib/calendarsStore";
import { useShellStore } from "../../lib/shellStore";
import { ColorSwatchPicker } from "./ColorSwatchPicker";
import { Button } from "../ui/Button";
import { buttonClasses } from "../ui/buttonClasses";
import { Input } from "../ui/Input";
import { fieldLabelClass } from "../ui/fieldStyles";

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

  // Submit on Enter from anywhere in the dialog (base-ui may leave focus on the
  // popup rather than a field — e.g. when editing, where the name is pre-filled),
  // but not from buttons (Cancel/Save/color swatches).
  function handleEnterToSave(event: KeyboardEvent) {
    const target = event.target as HTMLElement;
    if (
      event.key === "Enter" &&
      !event.shiftKey &&
      target.tagName !== "BUTTON" &&
      target.tagName !== "TEXTAREA"
    ) {
      event.preventDefault();
      handleSave();
    }
  }

  return (
    <Dialog.Root
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Portal>
        <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
        <Dialog.Popup
          onKeyDown={handleEnterToSave}
          className="fixed top-1/2 left-1/2 z-50 w-80 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3"
        >
          <Dialog.Title className="text-heading font-medium text-ink">
            {mode === "edit" ? "Edit calendar" : "New calendar"}
          </Dialog.Title>

          <form
            onSubmit={(event) => {
              event.preventDefault();
              handleSave();
            }}
          >
          <Input
            label="Name"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Calendar name"
            className="mt-4"
          />

          <div className="mt-4">
            <p className={fieldLabelClass}>Color</p>
            <div className="mt-2">
              <ColorSwatchPicker value={color} onValueChange={setColor} />
            </div>
          </div>

          <div className="mt-5 flex justify-end gap-2">
            <Dialog.Close
              className={buttonClasses({
                variant: "outline",
                color: "secondary",
                size: "small",
              })}
            >
              Cancel
            </Dialog.Close>
            <Button type="submit" size="small" disabled={!canSave}>
              Save
            </Button>
          </div>
          </form>
        </Dialog.Popup>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
