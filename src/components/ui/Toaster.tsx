import { Toast } from "@base-ui/react/toast";
import { toastManager } from "../../lib/toast";

function ToastList() {
  const { toasts } = Toast.useToastManager();

  return toasts.map((toastItem) => (
    <Toast.Root
      key={toastItem.id}
      toast={toastItem}
      className="w-80 rounded-shell-sm border border-border bg-surface p-3 shadow-elevation-3 data-[type=error]:border-calendar-tomato"
    >
      <Toast.Title className="text-body text-ink" />
      <Toast.Close
        aria-label="Dismiss"
        className="absolute top-2 right-2 rounded-shell-pill p-1 text-ink-muted hover:bg-surface-hover"
      >
        &times;
      </Toast.Close>
    </Toast.Root>
  ));
}

export function Toaster() {
  return (
    <Toast.Provider toastManager={toastManager}>
      <Toast.Portal>
        <Toast.Viewport className="fixed bottom-4 right-4 z-50 flex flex-col gap-2">
          <ToastList />
        </Toast.Viewport>
      </Toast.Portal>
    </Toast.Provider>
  );
}
