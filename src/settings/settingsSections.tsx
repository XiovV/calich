import { AccountSection } from "./AccountSection";
import { AccountsSection } from "./AccountsSection";
import { AppPasswordsSection } from "./AppPasswordsSection";
import { ReminderDeliverySection } from "./ReminderDeliverySection";
import { ImportExportSection } from "./ImportExportSection";

// The single source of truth for Settings' left-hand nav (#112): both the
// route table in App.tsx and the nav list in SettingsPage read this. Accounts
// (#119) is appended only for an Admin, so a non-Admin sees no Accounts item
// and has no route to reach it directly.
export function getSettingsSections(isAdmin: boolean) {
  const sections = [
    { path: "account", label: "Account", element: <AccountSection /> },
    { path: "app-passwords", label: "App passwords", element: <AppPasswordsSection /> },
    { path: "reminders", label: "Reminder delivery", element: <ReminderDeliverySection /> },
    { path: "import-export", label: "Import & export", element: <ImportExportSection /> },
  ];

  if (isAdmin) {
    sections.push({ path: "accounts", label: "Accounts", element: <AccountsSection /> });
  }

  return sections;
}
