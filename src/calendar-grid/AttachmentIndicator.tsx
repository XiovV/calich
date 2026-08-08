import { Paperclip } from "lucide-react";

interface AttachmentIndicatorProps {
  hasAttachments: boolean;
}

export function AttachmentIndicator({ hasAttachments }: AttachmentIndicatorProps) {
  if (!hasAttachments) return null;
  return <Paperclip className="ml-auto size-3 shrink-0 opacity-90" />;
}
