import { useState } from "react";
import { startOfDay } from "date-fns";
import { TopBar } from "./TopBar";
import { Sidebar } from "./Sidebar";
import { WeekGrid } from "../../calendar-grid/WeekGrid";
import { EventModal } from "../../calendar-grid/EventModal";
import { computeDefaultDraft, type DraftBlock } from "../../lib/gridTime";

interface PendingDraft {
  day: Date;
  draft: DraftBlock;
}

export function AppShell() {
  const [pendingDraft, setPendingDraft] = useState<PendingDraft | null>(null);

  function handleCreateClick() {
    const draft = computeDefaultDraft(new Date());
    setPendingDraft({ day: startOfDay(draft.start), draft });
  }

  return (
    <div className="flex h-screen flex-col bg-surface text-ink">
      <header className="h-16 shrink-0 border-b border-border bg-surface shadow-elevation-1">
        <TopBar />
      </header>
      <div className="flex flex-1 overflow-hidden">
        <nav className="w-72 shrink-0 border-r border-border bg-surface-sunken">
          <Sidebar onCreateClick={handleCreateClick} />
        </nav>
        <main className="m-3 flex-1 overflow-hidden rounded-shell-lg bg-surface shadow-elevation-1">
          <WeekGrid
            onDraftCreated={(day, draft) => setPendingDraft({ day, draft })}
          />
        </main>
      </div>
      {pendingDraft && (
        <EventModal
          key={pendingDraft.draft.start.getTime()}
          day={pendingDraft.day}
          draft={pendingDraft.draft}
          onClose={() => setPendingDraft(null)}
        />
      )}
    </div>
  );
}
