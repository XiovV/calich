import { ArrowLeft } from "lucide-react";
import { useNavigate } from "react-router";
import { IconButton } from "../components/ui/IconButton";
import { AccountEmailSection } from "./AccountEmailSection";
import { AppPasswordsSection } from "./AppPasswordsSection";
import { ImportExportSection } from "./ImportExportSection";
import { ReminderDeliverySection } from "./ReminderDeliverySection";

// The Settings page shell. Timezone and password sections will be dropped
// into the content area below by later work.
export function SettingsPage() {
  const navigate = useNavigate();

  return (
    <div className="flex h-screen flex-col bg-surface text-ink">
      <header className="flex h-16 shrink-0 items-center gap-4 border-b border-border bg-surface px-4">
        <IconButton
          onClick={() => navigate("/")}
          aria-label="Back to calendar"
        >
          <ArrowLeft className="size-5" />
        </IconButton>
        <span className="text-heading font-medium text-ink">Settings</span>
      </header>
      <main className="flex-1 overflow-y-auto p-6">
        <AccountEmailSection />
        <AppPasswordsSection />
        <ReminderDeliverySection />
        <ImportExportSection />
      </main>
    </div>
  );
}
