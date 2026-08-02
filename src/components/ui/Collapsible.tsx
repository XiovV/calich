import type { ReactNode } from "react";
import { Collapsible as BaseCollapsible } from "@base-ui/react/collapsible";
import { ChevronDown } from "lucide-react";

interface CollapsibleProps {
  label: string;
  defaultOpen?: boolean;
  children: ReactNode;
}

export function Collapsible({
  label,
  defaultOpen = true,
  children,
}: CollapsibleProps) {
  return (
    <BaseCollapsible.Root defaultOpen={defaultOpen}>
      <BaseCollapsible.Trigger className="group flex w-full items-center gap-1 px-2 py-1.5 text-label-sm font-medium text-ink-muted">
        <ChevronDown className="size-4 -rotate-90 transition-transform group-data-[panel-open]:rotate-0" />
        {label}
      </BaseCollapsible.Trigger>
      <BaseCollapsible.Panel>{children}</BaseCollapsible.Panel>
    </BaseCollapsible.Root>
  );
}
