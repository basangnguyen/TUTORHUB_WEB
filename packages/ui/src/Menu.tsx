import { Check, ChevronRight, Circle } from "lucide-react";
import { DropdownMenu } from "radix-ui";
import { forwardRef, type ComponentPropsWithoutRef } from "react";
import { cx } from "./utils";

export const Menu = DropdownMenu.Root;
export const MenuTrigger = DropdownMenu.Trigger;
export const MenuGroup = DropdownMenu.Group;
export const MenuRadioGroup = DropdownMenu.RadioGroup;

export const MenuContent = forwardRef<
  HTMLDivElement,
  ComponentPropsWithoutRef<typeof DropdownMenu.Content>
>(function MenuContent({ className, sideOffset = 6, ...props }, ref) {
  return (
    <DropdownMenu.Portal>
      <DropdownMenu.Content
        {...props}
        className={cx("th-menu-content", className)}
        ref={ref}
        sideOffset={sideOffset}
      />
    </DropdownMenu.Portal>
  );
});

export interface MenuItemProps extends ComponentPropsWithoutRef<
  typeof DropdownMenu.Item
> {
  inset?: boolean;
  tone?: "default" | "danger";
}

export const MenuItem = forwardRef<HTMLDivElement, MenuItemProps>(
  function MenuItem(
    { className, inset = false, tone = "default", ...props },
    ref,
  ) {
    return (
      <DropdownMenu.Item
        {...props}
        className={cx(
          "th-menu-item",
          inset && "th-menu-item--inset",
          tone === "danger" && "th-menu-item--danger",
          className,
        )}
        ref={ref}
      />
    );
  },
);

export const MenuCheckboxItem = forwardRef<
  HTMLDivElement,
  ComponentPropsWithoutRef<typeof DropdownMenu.CheckboxItem>
>(function MenuCheckboxItem({ children, className, ...props }, ref) {
  return (
    <DropdownMenu.CheckboxItem
      {...props}
      className={cx("th-menu-item th-menu-item--checkable", className)}
      ref={ref}
    >
      <span className="th-menu-item__indicator">
        <DropdownMenu.ItemIndicator>
          <Check aria-hidden="true" />
        </DropdownMenu.ItemIndicator>
      </span>
      {children}
    </DropdownMenu.CheckboxItem>
  );
});

export const MenuRadioItem = forwardRef<
  HTMLDivElement,
  ComponentPropsWithoutRef<typeof DropdownMenu.RadioItem>
>(function MenuRadioItem({ children, className, ...props }, ref) {
  return (
    <DropdownMenu.RadioItem
      {...props}
      className={cx("th-menu-item th-menu-item--checkable", className)}
      ref={ref}
    >
      <span className="th-menu-item__indicator">
        <DropdownMenu.ItemIndicator>
          <Circle aria-hidden="true" fill="currentColor" />
        </DropdownMenu.ItemIndicator>
      </span>
      {children}
    </DropdownMenu.RadioItem>
  );
});

export const MenuLabel = forwardRef<
  HTMLDivElement,
  ComponentPropsWithoutRef<typeof DropdownMenu.Label>
>(function MenuLabel({ className, ...props }, ref) {
  return (
    <DropdownMenu.Label
      {...props}
      className={cx("th-menu-label", className)}
      ref={ref}
    />
  );
});

export const MenuSeparator = forwardRef<
  HTMLDivElement,
  ComponentPropsWithoutRef<typeof DropdownMenu.Separator>
>(function MenuSeparator({ className, ...props }, ref) {
  return (
    <DropdownMenu.Separator
      {...props}
      className={cx("th-menu-separator", className)}
      ref={ref}
    />
  );
});

export const MenuSub = DropdownMenu.Sub;
export const MenuSubTrigger = forwardRef<
  HTMLDivElement,
  ComponentPropsWithoutRef<typeof DropdownMenu.SubTrigger>
>(function MenuSubTrigger({ children, className, ...props }, ref) {
  return (
    <DropdownMenu.SubTrigger
      {...props}
      className={cx("th-menu-item", className)}
      ref={ref}
    >
      {children}
      <ChevronRight aria-hidden="true" className="th-menu-item__end-icon" />
    </DropdownMenu.SubTrigger>
  );
});

export const MenuSubContent = forwardRef<
  HTMLDivElement,
  ComponentPropsWithoutRef<typeof DropdownMenu.SubContent>
>(function MenuSubContent({ className, ...props }, ref) {
  return (
    <DropdownMenu.SubContent
      {...props}
      className={cx("th-menu-content", className)}
      ref={ref}
    />
  );
});
