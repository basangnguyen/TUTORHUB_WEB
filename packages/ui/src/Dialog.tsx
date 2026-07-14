import { X } from "lucide-react";
import { Dialog as DialogPrimitive } from "radix-ui";
import { forwardRef, type ComponentPropsWithoutRef } from "react";
import { cx } from "./utils";

export const Dialog = DialogPrimitive.Root;
export const DialogTrigger = DialogPrimitive.Trigger;
export const DialogClose = DialogPrimitive.Close;

export const DialogTitle = forwardRef<
  HTMLHeadingElement,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(function DialogTitle({ className, ...props }, ref) {
  return (
    <DialogPrimitive.Title
      {...props}
      className={cx("th-dialog-title", className)}
      ref={ref}
    />
  );
});

export const DialogDescription = forwardRef<
  HTMLParagraphElement,
  ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(function DialogDescription({ className, ...props }, ref) {
  return (
    <DialogPrimitive.Description
      {...props}
      className={cx("th-dialog-description", className)}
      ref={ref}
    />
  );
});

export interface DialogContentProps extends ComponentPropsWithoutRef<
  typeof DialogPrimitive.Content
> {
  closeLabel?: string;
  hideClose?: boolean;
}

export const DialogContent = forwardRef<HTMLDivElement, DialogContentProps>(
  function DialogContent(
    { children, className, closeLabel = "Đóng", hideClose = false, ...props },
    ref,
  ) {
    return (
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="th-dialog-overlay" />
        <DialogPrimitive.Content
          {...props}
          className={cx("th-dialog-content", className)}
          ref={ref}
        >
          {children}
          {!hideClose && (
            <DialogPrimitive.Close
              aria-label={closeLabel}
              className="th-dialog-close"
            >
              <X aria-hidden="true" />
            </DialogPrimitive.Close>
          )}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    );
  },
);

export const DialogFooter = forwardRef<
  HTMLDivElement,
  ComponentPropsWithoutRef<"div">
>(function DialogFooter({ className, ...props }, ref) {
  return (
    <div {...props} className={cx("th-dialog-footer", className)} ref={ref} />
  );
});

export const Drawer = DialogPrimitive.Root;
export const DrawerTrigger = DialogPrimitive.Trigger;
export const DrawerClose = DialogPrimitive.Close;
export const DrawerTitle = DialogTitle;
export const DrawerDescription = DialogDescription;

export interface DrawerContentProps extends DialogContentProps {
  side?: "left" | "right";
}

export const DrawerContent = forwardRef<HTMLDivElement, DrawerContentProps>(
  function DrawerContent({ className, side = "right", ...props }, ref) {
    return (
      <DialogContent
        {...props}
        className={cx(
          "th-drawer-content",
          `th-drawer-content--${side}`,
          className,
        )}
        ref={ref}
      />
    );
  },
);
