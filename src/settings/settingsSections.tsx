import type { ReactElement } from "react";
import {
  ArrowDownUp,
  Bell,
  Boxes,
  Cable,
  KeyRound,
  SlidersHorizontal,
  UserRound,
  UsersRound,
  type LucideIcon,
} from "lucide-react";
import { AccountSection } from "./AccountSection";
import { AppPasswordsSection } from "./AppPasswordsSection";
import { ConnectionsSection } from "./ConnectionsSection";
import { PreferencesSection } from "./PreferencesSection";
import { ReminderDeliverySection } from "./ReminderDeliverySection";
import { ImportExportSection } from "./ImportExportSection";
import { MembersSection } from "./MembersSection";
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
  icon: LucideIcon;
  element: ReactElement;
}

// The single source of truth for Settings' left-hand nav (#112, #176): both
// the route table in App.tsx and the rail in SettingsModal read this. Group
// follows how the code is already scoped (ADR-0049) — Members and Groups
// are per-Workspace (ADR-0045), the rest are per-User. Preferences (#128,
// ADR-0039) leads the Personal group. icon lives here rather than a parallel
// lookup map (#190) — none is a gear (that's Settings itself), and Groups
// deliberately drops the person glyph so Account and Members (the rail's
// only two people-shaped concepts) never sit adjacent to a third. Connections
// (#285) sits right after Account, distinctly iconed and labelled, so a
// third-party Provider grant is never confused with the User's own login.
export function getSettingsSections(): SettingsSection[] {
  return [
    { path: "preferences", label: "Preferences", group: "personal", icon: SlidersHorizontal, element: <PreferencesSection /> },
    { path: "account", label: "Account", group: "personal", icon: UserRound, element: <AccountSection /> },
    { path: "connections", label: "Connections", group: "personal", icon: Cable, element: <ConnectionsSection /> },
    { path: "members", label: "Members", group: "workspace", icon: UsersRound, element: <MembersSection /> },
    { path: "groups", label: "Groups", group: "workspace", icon: Boxes, element: <GroupsSection /> },
    { path: "app-passwords", label: "App passwords", group: "personal", icon: KeyRound, element: <AppPasswordsSection /> },
    { path: "reminders", label: "Reminder delivery", group: "personal", icon: Bell, element: <ReminderDeliverySection /> },
    { path: "import-export", label: "Import & export", group: "personal", icon: ArrowDownUp, element: <ImportExportSection /> },
  ];
}
