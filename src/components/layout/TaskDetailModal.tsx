import { useState } from "react";
import { Dialog } from "@base-ui/react/dialog";
import { Trash2, X } from "lucide-react";
import { dateInputValueToDate, dateToDateInputValue } from "../../lib/eventTimeRange";
import {
  PRIORITY_LEVELS,
  PRIORITY_LEVEL_LABELS,
  priorityLevelFromValue,
  priorityValueForLevel,
  type PriorityLevel,
} from "../../lib/taskPriority";
import { useTaskListsStore } from "../../lib/taskListsStore";
import { useTasksStore } from "../../lib/tasksStore";
import type { Task } from "../../lib/tasksApi";
import { toast } from "../../lib/toast";
import { DeleteTaskConfirmation } from "./DeleteTaskConfirmation";
import { buttonClasses } from "../ui/buttonClasses";
import { IconButton } from "../ui/IconButton";
import { Input } from "../ui/Input";
import { Select } from "../ui/Select";
import { Textarea } from "../ui/Textarea";

interface TaskDetailModalProps {
  task: Task;
  onClose: () => void;
}

// TaskDetailModal is the small, quick detail surface a Task row opens into
// (#311, ADR-0083) — deliberately not EventModal, which carries progressive
// disclosure (ADR-0056), the Save plan (ADR-0066), scoped series edits,
// Attendees, Attachments, Reminders and recurrence, none of which a Task
// has. Every field here commits on its own as the User interacts with it —
// there is no Save, only `Done` (ADR-0068) — because by the time the
// footer is visible, every write above it has already happened.
export function TaskDetailModal({ task, onClose }: TaskDetailModalProps) {
  const taskLists = useTaskListsStore((state) => state.taskLists);
  const updateTaskNotes = useTasksStore((state) => state.updateTaskNotes);
  const setTaskDeadline = useTasksStore((state) => state.setTaskDeadline);
  const clearTaskDeadline = useTasksStore((state) => state.clearTaskDeadline);
  const updateTaskPriority = useTasksStore((state) => state.updateTaskPriority);
  const moveTask = useTasksStore((state) => state.moveTask);
  const deleteTask = useTasksStore((state) => state.deleteTask);

  // Seeded once from `task` and never re-synced from it here — TasksPanel
  // remounts this component (via `key={task.id}`) whenever a different Task
  // opens, which is what actually guards against carrying one Task's
  // half-typed notes into another's, without a setState-in-effect.
  const [notes, setNotes] = useState(task.notes);
  const [isDeleteConfirmOpen, setIsDeleteConfirmOpen] = useState(false);

  function commitNotes() {
    if (notes === task.notes) return;

    updateTaskNotes(task.id, notes).catch(() => {
      toast.error("Couldn't save the notes.");
      setNotes(task.notes);
    });
  }

  function handleDeadlineChange(value: string) {
    if (!value) return;
    setTaskDeadline(task.id, dateInputValueToDate(value)).catch(() =>
      toast.error("Couldn't set the deadline."),
    );
  }

  function handleClearDeadline() {
    clearTaskDeadline(task.id).catch(() => toast.error("Couldn't clear the deadline."));
  }

  function handlePriorityChange(level: PriorityLevel) {
    updateTaskPriority(task.id, priorityValueForLevel(level)).catch(() =>
      toast.error("Couldn't update the priority."),
    );
  }

  function handleTaskListChange(value: string) {
    const taskListId = Number(value);
    if (taskListId === task.taskListId) return;

    moveTask(task.id, taskListId).catch(() => toast.error("Couldn't move the task."));
  }

  async function handleConfirmDelete() {
    setIsDeleteConfirmOpen(false);
    try {
      await deleteTask(task.id);
      onClose();
    } catch {
      toast.error("Couldn't delete the task.");
    }
  }

  return (
    <>
      <Dialog.Root
        open
        onOpenChange={(open) => {
          if (!open) onClose();
        }}
      >
        <Dialog.Portal>
          <Dialog.Backdrop className="fixed inset-0 z-40 bg-ink/20" />
          <Dialog.Popup className="fixed top-1/2 left-1/2 z-50 w-96 -translate-x-1/2 -translate-y-1/2 rounded-shell-lg bg-surface p-5 shadow-elevation-3">
            <div className="flex items-center justify-between gap-2">
              <Dialog.Title className="min-w-0 flex-1 truncate text-heading font-medium text-ink">
                {task.title}
              </Dialog.Title>
              <IconButton
                size="small"
                color="danger"
                aria-label="Delete task"
                onClick={() => setIsDeleteConfirmOpen(true)}
              >
                <Trash2 className="size-4" />
              </IconButton>
            </div>

            <Textarea
              label="Notes"
              value={notes}
              onChange={(event) => setNotes(event.target.value)}
              onBlur={commitNotes}
              placeholder="Add notes"
              className="mt-4"
            />

            <div className="mt-4 flex items-end gap-2">
              <Input
                label="Deadline"
                type="date"
                value={task.due ? dateToDateInputValue(task.due) : ""}
                onChange={(event) => handleDeadlineChange(event.target.value)}
                className="flex-1"
              />
              {task.due && (
                <IconButton
                  size="small"
                  aria-label="Clear deadline"
                  onClick={handleClearDeadline}
                >
                  <X className="size-4" />
                </IconButton>
              )}
            </div>

            <Select
              label="Priority"
              value={priorityLevelFromValue(task.priority)}
              onValueChange={handlePriorityChange}
              options={PRIORITY_LEVELS.map((level) => ({
                value: level,
                label: PRIORITY_LEVEL_LABELS[level],
              }))}
              className="mt-4"
            />

            <Select
              label="Task list"
              value={String(task.taskListId)}
              onValueChange={handleTaskListChange}
              options={taskLists.map((taskList) => ({
                value: String(taskList.id),
                label: taskList.name,
              }))}
              className="mt-4"
            />

            <div className="mt-5 flex justify-end">
              <Dialog.Close
                className={buttonClasses({
                  variant: "outline",
                  color: "secondary",
                  size: "small",
                })}
              >
                Done
              </Dialog.Close>
            </div>
          </Dialog.Popup>
        </Dialog.Portal>
      </Dialog.Root>
      {isDeleteConfirmOpen && (
        <DeleteTaskConfirmation
          task={task}
          onConfirm={handleConfirmDelete}
          onClose={() => setIsDeleteConfirmOpen(false)}
        />
      )}
    </>
  );
}
