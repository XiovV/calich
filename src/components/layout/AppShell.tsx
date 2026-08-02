export function AppShell() {
  return (
    <div className="flex h-screen flex-col bg-surface text-ink">
      <header className="h-16 shrink-0 border-b border-border bg-surface shadow-elevation-1" />
      <div className="flex flex-1 overflow-hidden">
        <nav className="w-72 shrink-0 border-r border-border bg-surface-sunken" />
        <main className="m-3 flex-1 overflow-auto rounded-shell-lg bg-surface shadow-elevation-1" />
      </div>
    </div>
  );
}
