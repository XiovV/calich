import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { IconButton } from "../components/ui/IconButton";

interface InviteLinkRevealProps {
  link: string;
  className?: string;
}

// Shown once, right after Creating or Reissuing a Workspace Invite
// (ADR-0044, #165): the token backing this link is never retrievable again,
// mirroring AppPasswordsSection's reveal-once secret and the retired
// account-level InviteLinkReveal it's modeled on.
export function InviteLinkReveal({ link, className }: InviteLinkRevealProps) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    await navigator.clipboard.writeText(link);
    setCopied(true);
  }

  return (
    <div className={`rounded-md border border-border bg-surface-raised p-3 ${className ?? ""}`}>
      <p className="text-label-sm text-ink-muted">Copy this now — it won't be shown again.</p>
      <div className="mt-2 flex items-center gap-2">
        <code className="flex-1 overflow-x-auto rounded bg-surface px-2 py-1 text-body">{link}</code>
        <IconButton onClick={handleCopy} aria-label="Copy invite link">
          {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
        </IconButton>
      </div>
    </div>
  );
}
