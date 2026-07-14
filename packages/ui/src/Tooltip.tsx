import { Tooltip as TooltipPrimitive } from "radix-ui";
import { type ReactElement, type ReactNode } from "react";

export const TooltipProvider = TooltipPrimitive.Provider;

export interface TooltipProps {
  children: ReactElement;
  content: ReactNode;
  delayDuration?: number;
  side?: "top" | "right" | "bottom" | "left";
}

export function Tooltip({
  children,
  content,
  delayDuration = 350,
  side = "top",
}: TooltipProps) {
  return (
    <TooltipPrimitive.Root delayDuration={delayDuration}>
      <TooltipPrimitive.Trigger asChild>{children}</TooltipPrimitive.Trigger>
      <TooltipPrimitive.Portal>
        <TooltipPrimitive.Content
          className="th-tooltip-content"
          side={side}
          sideOffset={6}
        >
          {content}
          <TooltipPrimitive.Arrow className="th-tooltip-arrow" />
        </TooltipPrimitive.Content>
      </TooltipPrimitive.Portal>
    </TooltipPrimitive.Root>
  );
}
