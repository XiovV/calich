import { AccountSection } from "./AccountSection";
import { AppPasswordsSection } from "./AppPasswordsSection";
import { PreferencesSection } from "./PreferencesSection";
import { ReminderDeliverySection } from "./ReminderDeliverySection";
import { ImportExportSection } from "./ImportExportSection";
import { WorkspaceSection } from "./WorkspaceSection";

// The single source of truth for Settings' left-hand nav (#112): both the
// route table in App.tsx and the nav list in SettingsPage read this.
// Preferences (#128, ADR-0039) leads the list.
export function getSettingsSections() {
  return [
    { path: "preferences", label: "Preferences", element: <PreferencesSection /> },
    { path: "account", label: "Account", element: <AccountSection /> },
    { path: "workspace", label: "Workspace", element: <WorkspaceSection /> },
    { path: "app-passwords", label: "App passwords", element: <AppPasswordsSection /> },
    { path: "reminders", label: "Reminder delivery", element: <ReminderDeliverySection /> },
    { path: "import-export", label: "Import & export", element: <ImportExportSection /> },
  ];
}
