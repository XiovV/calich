import type { ReactElement } from "react";
import { AccountSection } from "./AccountSection";
import { AppPasswordsSection } from "./AppPasswordsSection";
import { PreferencesSection } from "./PreferencesSection";
import { ReminderDeliverySection } from "./ReminderDeliverySection";
import { ImportExportSection } from "./ImportExportSection";
import { WorkspaceSection } from "./WorkspaceSection";
import { GroupsSection } from "./GroupsSection";

export type SettingsGroup = "personal" | "workspace";

// Display order and label for each SettingsGroup, kept beside the type so a
// third group never needs updating in two places.
export const SETTINGS_GROUP_ORDER: SettingsGroup[] = ["personal", "workspace"];
export const SETTINGS_GROUP_LABELS: Record<SettingsGroup, string> = {
  personal: "Personal",
  workspace: "Workspace",
};

export interface SettingsSection {
  path: string;
  label: string;
  group: SettingsGroup;
  element: ReactElement;
}

// The single source of truth for Settings' left-hand nav (#112, #176): both
// the route table in App.tsx and the rail in SettingsModal read this. Group
// follows how the code is already scoped (ADR-0049) — Workspace and Groups
// are per-Workspace (ADR-0045), the rest are per-User. Preferences (#128,
// ADR-0039) leads the Personal group.
export function getSettingsSections(): SettingsSection[] {
  return [
    { path: "preferences", label: "Preferences", group: "personal", element: <PreferencesSection /> },
    { path: "account", label: "Account", group: "personal", element: <AccountSection /> },
    { path: "workspace", label: "Workspace", group: "workspace", element: <WorkspaceSection /> },
    { path: "groups", label: "Groups", group: "workspace", element: <GroupsSection /> },
    { path: "app-passwords", label: "App passwords", group: "personal", element: <AppPasswordsSection /> },
    { path: "reminders", label: "Reminder delivery", group: "personal", element: <ReminderDeliverySection /> },
    { path: "import-export", label: "Import & export", group: "personal", element: <ImportExportSection /> },
  ];
}
