import type { PropsWithChildren } from "react";
import "./status-badge.css";

export interface StatusBadgeProps extends PropsWithChildren {
  tone: "neutral" | "success" | "danger";
}

export function StatusBadge({ children, tone }: StatusBadgeProps) {
  return (
    <span className={`status-badge status-badge--${tone}`}>{children}</span>
  );
}
