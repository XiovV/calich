# Separate events store from shell store

Events (the calendar's actual content — created, and later edited/deleted) live in a new `useEventsStore`, not in `useShellStore`. `useShellStore` (ADR-0001) exists for small, cohesive shell-chrome state; events are domain content that will accumulate real CRUD logic of its own, and mixing the two would turn the shell store into a catch-all for "anything Zustand."
