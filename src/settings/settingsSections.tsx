import { AccountEmailSection } from "./AccountEmailSection";
import { AppPasswordsSection } from "./AppPasswordsSection";
import { ReminderDeliverySection } from "./ReminderDeliverySection";
import { ImportExportSection } from "./ImportExportSection";

// The single source of truth for Settings' left-hand nav (#112): both the
// route table in App.tsx and the nav list in SettingsPage read this, so
// adding a section (starting with account administration, #109) only means
// adding one entry here.
export const SETTINGS_SECTIONS = [
  { path: "account", label: "Account email", element: <AccountEmailSection /> },
  { path: "app-passwords", label: "App passwords", element: <AppPasswordsSection /> },
  { path: "reminders", label: "Reminder delivery", element: <ReminderDeliverySection /> },
  { path: "import-export", label: "Import & export", element: <ImportExportSection /> },
];
