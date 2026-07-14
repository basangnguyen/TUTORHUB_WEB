import {
  forwardRef,
  useId,
  type InputHTMLAttributes,
  type TextareaHTMLAttributes,
} from "react";
import { cx } from "./utils";

interface FieldChromeProps {
  className?: string;
  error?: string;
  hint?: string;
  label: string;
}

export interface TextFieldProps
  extends
    Omit<InputHTMLAttributes<HTMLInputElement>, "size">,
    FieldChromeProps {}

export const TextField = forwardRef<HTMLInputElement, TextFieldProps>(
  function TextField({ className, error, hint, id, label, ...props }, ref) {
    const generatedId = useId();
    const inputId = id ?? generatedId;
    const hintId = hint ? `${inputId}-hint` : undefined;
    const errorId = error ? `${inputId}-error` : undefined;
    const describedBy = [props["aria-describedby"], hintId, errorId]
      .filter(Boolean)
      .join(" ");

    return (
      <div className={cx("th-field", className)}>
        <label className="th-field__label" htmlFor={inputId}>
          {label}
        </label>
        <input
          {...props}
          aria-describedby={describedBy || undefined}
          aria-invalid={Boolean(error) || undefined}
          className="th-field__control"
          id={inputId}
          ref={ref}
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
  },
);

export interface TextAreaFieldProps
  extends TextareaHTMLAttributes<HTMLTextAreaElement>, FieldChromeProps {}

export const TextAreaField = forwardRef<
  HTMLTextAreaElement,
  TextAreaFieldProps
>(function TextAreaField({ className, error, hint, id, label, ...props }, ref) {
  const generatedId = useId();
  const inputId = id ?? generatedId;
  const hintId = hint ? `${inputId}-hint` : undefined;
  const errorId = error ? `${inputId}-error` : undefined;
  const describedBy = [props["aria-describedby"], hintId, errorId]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={cx("th-field", className)}>
      <label className="th-field__label" htmlFor={inputId}>
        {label}
      </label>
      <textarea
        {...props}
        aria-describedby={describedBy || undefined}
        aria-invalid={Boolean(error) || undefined}
        className="th-field__control th-field__textarea"
        id={inputId}
        ref={ref}
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
});
