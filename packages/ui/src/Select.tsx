import { Check, ChevronDown, ChevronUp } from "lucide-react";
import { Select as SelectPrimitive } from "radix-ui";
import { useId, type ComponentPropsWithoutRef } from "react";
import { cx } from "./utils";

export interface SelectOption {
  disabled?: boolean;
  label: string;
  value: string;
}

export interface SelectProps {
  ariaDescribedBy?: string;
  ariaInvalid?: boolean;
  ariaLabel: string;
  className?: string;
  defaultValue?: string;
  disabled?: boolean;
  name?: string;
  onValueChange?: (value: string) => void;
  options: readonly SelectOption[];
  placeholder?: string;
  triggerId?: string;
  value?: string;
}

export function Select({
  ariaDescribedBy,
  ariaInvalid,
  ariaLabel,
  className,
  defaultValue,
  disabled,
  name,
  onValueChange,
  options,
  placeholder,
  triggerId,
  value,
}: SelectProps) {
  return (
    <SelectPrimitive.Root
      defaultValue={defaultValue}
      disabled={disabled}
      name={name}
      onValueChange={onValueChange}
      value={value}
    >
      <SelectPrimitive.Trigger
        aria-describedby={ariaDescribedBy}
        aria-invalid={ariaInvalid || undefined}
        aria-label={ariaLabel}
        className={cx("th-select", className)}
        id={triggerId}
      >
        <SelectPrimitive.Value placeholder={placeholder} />
        <SelectPrimitive.Icon asChild>
          <ChevronDown aria-hidden="true" />
        </SelectPrimitive.Icon>
      </SelectPrimitive.Trigger>
      <SelectPrimitive.Portal>
        <SelectPrimitive.Content
          className="th-select-content"
          position="popper"
          sideOffset={6}
        >
          <SelectPrimitive.ScrollUpButton className="th-select-scroll-button">
            <ChevronUp aria-hidden="true" />
          </SelectPrimitive.ScrollUpButton>
          <SelectPrimitive.Viewport className="th-select-viewport">
            {options.map((option) => (
              <SelectPrimitive.Item
                className="th-select-item"
                disabled={option.disabled}
                key={option.value}
                value={option.value}
              >
                <SelectPrimitive.ItemText>
                  {option.label}
                </SelectPrimitive.ItemText>
                <SelectPrimitive.ItemIndicator className="th-select-item__indicator">
                  <Check aria-hidden="true" />
                </SelectPrimitive.ItemIndicator>
              </SelectPrimitive.Item>
            ))}
          </SelectPrimitive.Viewport>
          <SelectPrimitive.ScrollDownButton className="th-select-scroll-button">
            <ChevronDown aria-hidden="true" />
          </SelectPrimitive.ScrollDownButton>
        </SelectPrimitive.Content>
      </SelectPrimitive.Portal>
    </SelectPrimitive.Root>
  );
}

export interface SelectFieldProps extends SelectProps {
  error?: string;
  hint?: string;
  label: string;
}

export function SelectField({
  error,
  hint,
  label,
  ...props
}: SelectFieldProps) {
  const generatedId = useId();
  const triggerId = props.triggerId ?? generatedId;
  const hintId = hint ? `${triggerId}-hint` : undefined;
  const errorId = error ? `${triggerId}-error` : undefined;
  const describedBy = [props.ariaDescribedBy, hintId, errorId]
    .filter(Boolean)
    .join(" ");

  return (
    <div className="th-field">
      <label className="th-field__label" htmlFor={triggerId}>
        {label}
      </label>
      <Select
        {...props}
        ariaDescribedBy={describedBy || undefined}
        ariaInvalid={Boolean(error)}
        triggerId={triggerId}
      />
      {hint && (
        <span className="th-field__hint" id={hintId}>
          {hint}
        </span>
      )}
      {error && (
        <span className="th-field__error" id={errorId} role="alert">
          {error}
        </span>
      )}
    </div>
  );
}

export type SelectContentProps = ComponentPropsWithoutRef<
  typeof SelectPrimitive.Content
>;
