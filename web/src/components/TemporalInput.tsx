import { CalendarBlank, Clock } from "@phosphor-icons/react";
import {
  useRef,
  type InputHTMLAttributes,
  type MouseEvent
} from "react";

type TemporalInputProps = Omit<InputHTMLAttributes<HTMLInputElement>, "type"> & {
  type: "date" | "datetime-local" | "time";
  pickerLabel?: string;
};

export function TemporalInput({
  type,
  pickerLabel,
  className,
  ...props
}: TemporalInputProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const Icon = type === "time" ? Clock : CalendarBlank;
  const label = pickerLabel
    ?? (type === "time" ? "选择时间" : type === "date" ? "选择日期" : "选择日期和时间");

  const openPicker = (event: MouseEvent<HTMLButtonElement>) => {
    event.preventDefault();
    const input = inputRef.current;
    if (!input || input.disabled || input.readOnly) return;
    input.focus();
    try {
      input.showPicker?.();
    } catch {
      input.click();
    }
  };

  return (
    <span className="temporal-input-shell">
      <input
        {...props}
        ref={inputRef}
        type={type}
        className={["temporal-input", className].filter(Boolean).join(" ")}
      />
      <button
        type="button"
        className="temporal-picker-button"
        aria-label={label}
        disabled={props.disabled}
        onClick={openPicker}
      >
        <Icon aria-hidden="true" size={18} weight="duotone" />
      </button>
    </span>
  );
}
