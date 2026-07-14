import { LoaderCircle } from "lucide-react";
import { forwardRef, type ButtonHTMLAttributes, type ReactNode } from "react";
import { cx } from "./utils";

export type ButtonVariant = "primary" | "secondary" | "quiet" | "danger";
export type ButtonSize = "sm" | "md" | "lg";

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  leadingIcon?: ReactNode;
  loading?: boolean;
  loadingLabel?: string;
  size?: ButtonSize;
  trailingIcon?: ReactNode;
  variant?: ButtonVariant;
}

export const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  function Button(
    {
      children,
      className,
      disabled,
      leadingIcon,
      loading = false,
      loadingLabel = "Đang xử lý",
      size = "md",
      trailingIcon,
      type = "button",
      variant = "primary",
      ...props
    },
    ref,
  ) {
    return (
      <button
        {...props}
        aria-busy={loading || undefined}
        className={cx(
          "th-button",
          `th-button--${variant}`,
          `th-button--${size}`,
          className,
        )}
        disabled={disabled || loading}
        ref={ref}
        type={type}
      >
        {loading ? (
          <LoaderCircle aria-hidden="true" className="th-button__spinner" />
        ) : (
          leadingIcon && (
            <span aria-hidden="true" className="th-button__icon">
              {leadingIcon}
            </span>
          )
        )}
        <span>{loading ? loadingLabel : children}</span>
        {!loading && trailingIcon && (
          <span aria-hidden="true" className="th-button__icon">
            {trailingIcon}
          </span>
        )}
      </button>
    );
  },
);

export interface IconButtonProps extends Omit<
  ButtonProps,
  "aria-label" | "children" | "leadingIcon" | "trailingIcon"
> {
  children: ReactNode;
  label: string;
}

export const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(
  function IconButton(
    {
      children,
      className,
      disabled,
      label,
      loading = false,
      size = "md",
      type = "button",
      variant = "quiet",
      ...props
    },
    ref,
  ) {
    return (
      <button
        {...props}
        aria-label={label}
        aria-busy={loading || undefined}
        className={cx(
          "th-button",
          "th-icon-button",
          `th-button--${variant}`,
          `th-button--${size}`,
          className,
        )}
        disabled={disabled || loading}
        ref={ref}
        type={type}
      >
        {loading ? (
          <LoaderCircle aria-hidden="true" className="th-button__spinner" />
        ) : (
          <span aria-hidden="true" className="th-icon-button__icon">
            {children}
          </span>
        )}
      </button>
    );
  },
);
