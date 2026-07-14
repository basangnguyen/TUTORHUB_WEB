import { X } from "lucide-react";
import { Toast as ToastPrimitive } from "radix-ui";
import {
  forwardRef,
  type ComponentPropsWithoutRef,
  type ReactNode,
} from "react";
import { cx } from "./utils";

export const ToastProvider = ToastPrimitive.Provider;

export interface ToastProps extends Omit<
  ComponentPropsWithoutRef<typeof ToastPrimitive.Root>,
  "title"
> {
  closeLabel?: string;
  description?: ReactNode;
  title: ReactNode;
  tone?: "neutral" | "success" | "danger";
}

export const Toast = forwardRef<HTMLLIElement, ToastProps>(function Toast(
  {
    className,
    closeLabel = "Đóng thông báo",
    description,
    title,
    tone = "neutral",
    ...props
  },
  ref,
) {
  return (
    <ToastPrimitive.Root
      {...props}
      className={cx("th-toast", `th-toast--${tone}`, className)}
      ref={ref}
    >
      <div className="th-toast__content">
        <ToastPrimitive.Title className="th-toast__title">
          {title}
        </ToastPrimitive.Title>
        {description && (
          <ToastPrimitive.Description className="th-toast__description">
            {description}
          </ToastPrimitive.Description>
        )}
      </div>
      <ToastPrimitive.Close aria-label={closeLabel} className="th-toast__close">
        <X aria-hidden="true" />
      </ToastPrimitive.Close>
    </ToastPrimitive.Root>
  );
});

export const ToastViewport = forwardRef<
  HTMLOListElement,
  ComponentPropsWithoutRef<typeof ToastPrimitive.Viewport>
>(function ToastViewport({ className, ...props }, ref) {
  return (
    <ToastPrimitive.Viewport
      {...props}
      className={cx("th-toast-viewport", className)}
      ref={ref}
    />
  );
});
