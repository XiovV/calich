import { Check, Copy } from "lucide-react";
import { useState } from "react";
import { Button } from "../components/ui/Button";
import { IconButton } from "../components/ui/IconButton";
import { errorMessage } from "../lib/errorMessage";
import { toast } from "../lib/toast";

interface InviteLinkRevealProps {
  link: string;
  emailAvailable: boolean;
  onSendEmail: () => Promise<void>;
  className?: string;
}

// Shown once, right after Creating or Reissuing an Invite (ADR-0042): the
// token backing this link is never retrievable again, mirroring
// AppPasswordsSection's reveal-once secret.
export function InviteLinkReveal({ link, emailAvailable, onSendEmail, className }: InviteLinkRevealProps) {
  const [copied, setCopied] = useState(false);
  const [sendingEmail, setSendingEmail] = useState(false);

  async function handleCopy() {
    await navigator.clipboard.writeText(link);
    setCopied(true);
  }

  async function handleSendEmail() {
    setSendingEmail(true);
    try {
      await onSendEmail();
      toast.success("Invite email sent.");
    } catch (err) {
      toast.error(errorMessage(err));
    } finally {
      setSendingEmail(false);
    }
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
      {emailAvailable && (
        <Button
          variant="outline"
          color="secondary"
          size="small"
          className="mt-2"
          loading={sendingEmail}
          onClick={handleSendEmail}
        >
          Send by email
        </Button>
      )}
    </div>
  );
}
