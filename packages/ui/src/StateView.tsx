import { Inbox, ShieldX, TriangleAlert, WifiOff } from "lucide-react";
import { type ReactNode } from "react";
import { cx } from "./utils";

export type StateViewTone = "empty" | "error" | "forbidden" | "offline";

const defaultIcons = {
  empty: Inbox,
  error: TriangleAlert,
  forbidden: ShieldX,
  offline: WifiOff,
} as const;

export interface StateViewProps {
  actions?: ReactNode;
  className?: string;
  description: ReactNode;
  icon?: ReactNode;
  title: ReactNode;
  tone?: StateViewTone;
}

export function StateView({
  actions,
  className,
  description,
  icon,
  title,
  tone = "empty",
}: StateViewProps) {
  const DefaultIcon = defaultIcons[tone];
  const role = tone === "error" || tone === "offline" ? "alert" : "status";

  return (
    <section
      className={cx("th-state-view", `th-state-view--${tone}`, className)}
      role={role}
    >
      <span aria-hidden="true" className="th-state-view__icon">
        {icon ?? <DefaultIcon />}
      </span>
      <div className="th-state-view__body">
        <h2>{title}</h2>
        <p>{description}</p>
      </div>
      {actions && <div className="th-state-view__actions">{actions}</div>}
    </section>
  );
}

export type StateViewVariantProps = Omit<StateViewProps, "tone">;

export const EmptyState = (props: StateViewVariantProps) => (
  <StateView {...props} tone="empty" />
);
export const ErrorState = (props: StateViewVariantProps) => (
  <StateView {...props} tone="error" />
);
export const ForbiddenState = (props: StateViewVariantProps) => (
  <StateView {...props} tone="forbidden" />
);
export const OfflineState = (props: StateViewVariantProps) => (
  <StateView {...props} tone="offline" />
);
