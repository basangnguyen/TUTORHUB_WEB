import { forwardRef, type ComponentPropsWithoutRef } from "react";
import { cx } from "./utils";

export interface SkeletonProps extends ComponentPropsWithoutRef<"span"> {
  height?: number | string;
  width?: number | string;
}

export const Skeleton = forwardRef<HTMLSpanElement, SkeletonProps>(
  function Skeleton({ className, height, style, width, ...props }, ref) {
    return (
      <span
        {...props}
        aria-hidden="true"
        className={cx("th-skeleton", className)}
        ref={ref}
        style={{ ...style, height, width }}
      />
    );
  },
);

export interface SkeletonGroupProps extends ComponentPropsWithoutRef<"div"> {
  label: string;
}

export const SkeletonGroup = forwardRef<HTMLDivElement, SkeletonGroupProps>(
  function SkeletonGroup({ className, label, ...props }, ref) {
    return (
      <div
        {...props}
        aria-label={label}
        className={cx("th-skeleton-group", className)}
        ref={ref}
        role="status"
      />
    );
  },
);
