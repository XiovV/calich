import { useEffect, useState } from "react";
import type { ReactNode } from "react";
import { Link, useParams } from "react-router";
import { publicIndexApi, type PublicIndex } from "../lib/publicIndexApi";

// The public index page (#325, ADR-0084, ADR-0087): a stranger with no
// Session opens /:handle (routed in App.tsx, outside ProtectedRoute) and
// sees the owner's Name and initials plus every Public Booking Link they
// hold, unioned across every Workspace. There is no row behind this page —
// it renders exactly what GET /api/public/{handle} answers, computed in
// exactly the sense CONTEXT.md's Task bucket is. An unknown Handle and a
// Handle with no Public links render identically (an empty index, no name)
// so the namespace cannot enumerate Users.

type IndexResult = { status: "ready"; index: PublicIndex } | { status: "error" };

export function PublicIndexPage() {
  const { handle = "" } = useParams<{ handle: string }>();

  const [result, setResult] = useState<IndexResult | null>(null);

  useEffect(() => {
    let cancelled = false;
    publicIndexApi
      .get(handle)
      .then((index) => {
        if (!cancelled) setResult({ status: "ready", index });
      })
      .catch(() => {
        if (!cancelled) setResult({ status: "error" });
      });
    return () => {
      cancelled = true;
    };
  }, [handle]);

  if (!result) {
    return (
      <PageShell>
        <p className="text-body text-ink-muted">Loading…</p>
      </PageShell>
    );
  }

  if (result.status === "error") {
    return (
      <PageShell>
        <h1 className="text-heading font-medium text-ink">Something went wrong.</h1>
        <p className="mt-1 text-body text-ink-muted">Please try again in a moment.</p>
      </PageShell>
    );
  }

  const { index } = result;

  if (index.links.length === 0) {
    return (
      <PageShell>
        <h1 className="text-heading font-medium text-ink">Nothing here yet.</h1>
        <p className="mt-1 text-body text-ink-muted">This page has no public booking links.</p>
      </PageShell>
    );
  }

  return (
    <PageShell>
      <div className="flex items-center gap-3">
        <div
          className="flex h-12 w-12 shrink-0 items-center justify-center rounded-shell-pill bg-accent-soft text-label-sm font-medium text-accent-ink"
          aria-hidden="true"
        >
          {initials(index.hostName)}
        </div>
        <h1 className="text-heading-lg font-medium text-ink">{index.hostName}</h1>
      </div>

      <ul className="mt-6 flex flex-col gap-3">
        {index.links.map((link) => (
          <li key={link.slug}>
            <Link
              to={`/${handle}/${link.slug}`}
              className="block rounded-shell-md border border-border px-4 py-3 transition-colors hover:bg-surface-hover"
            >
              <p className="text-body font-medium text-ink">{link.title}</p>
              <p className="text-label-sm text-ink-muted">{link.durationMinutes} min</p>
            </Link>
          </li>
        ))}
      </ul>
    </PageShell>
  );
}

// initials derives an avatar label from a display Name (ADR-0047: not
// unique, freeform) — the first letter of the first and last word, or the
// first two letters of a single-word Name. Presentational only; nothing
// here is stored or compared.
function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (words.length === 0) return "";
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
  return (words[0][0] + words[words.length - 1][0]).toUpperCase();
}

function PageShell({ children }: { children: ReactNode }) {
  return (
    <div className="min-h-screen bg-surface px-4 py-10 sm:px-8">
      <div className="mx-auto max-w-3xl">{children}</div>
    </div>
  );
}
