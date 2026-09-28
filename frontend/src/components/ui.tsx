import { X } from "lucide-preact";
import type { ComponentChildren } from "preact";
import { useEffect, useRef } from "preact/hooks";

/** A modal built on <dialog>, so focus trapping and Escape come for free. */
export function Dialog(props: {
  title: string;
  onClose: () => void;
  children: ComponentChildren;
  footer?: ComponentChildren;
  wide?: boolean;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current!;
    dialog.showModal();
    return () => dialog.close();
  }, []);
  return (
    <dialog
      ref={ref}
      class={`dialog ${props.wide ? "dialog-wide" : ""}`}
      onCancel={(e) => {
        e.preventDefault();
        props.onClose();
      }}
      onClick={(e) => e.target === ref.current && props.onClose()}
    >
      <div class="dialog-inner">
        <div class="dialog-head">
          <h2>{props.title}</h2>
          <button class="icon-btn" onClick={props.onClose} aria-label="Close">
            <X size={16} />
          </button>
        </div>
        <div class="dialog-body">{props.children}</div>
        {props.footer && <div class="dialog-foot">{props.footer}</div>}
      </div>
    </dialog>
  );
}

export function Field(props: { label: string; hint?: string; children: ComponentChildren }) {
  return (
    <label class="field">
      <span class="field-label">{props.label}</span>
      {props.children}
      {props.hint && <span class="field-hint">{props.hint}</span>}
    </label>
  );
}

export function TextInput(props: {
  value: string | undefined;
  onChange: (v: string) => void;
  placeholder?: string;
  type?: string;
  list?: string;
  autofocus?: boolean;
}) {
  return (
    <input
      class="input"
      type={props.type ?? "text"}
      value={props.value ?? ""}
      placeholder={props.placeholder}
      list={props.list}
      autofocus={props.autofocus}
      spellcheck={false}
      onInput={(e) => props.onChange(e.currentTarget.value)}
    />
  );
}

export function Select<T extends string>(props: {
  value: T;
  options: readonly (T | [T, string])[];
  onChange: (v: T) => void;
}) {
  return (
    <select
      class="input"
      value={props.value}
      onChange={(e) => props.onChange(e.currentTarget.value as T)}
    >
      {props.options.map((o) => {
        const [value, label] = Array.isArray(o) ? o : [o, o];
        return (
          <option key={value} value={value}>
            {label}
          </option>
        );
      })}
    </select>
  );
}

export function Toggle(props: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <label class="toggle">
      <input
        type="checkbox"
        checked={props.checked}
        onChange={(e) => props.onChange(e.currentTarget.checked)}
      />
      <span class="toggle-track" />
      <span>{props.label}</span>
    </label>
  );
}

/** Two-way option picker shown as a row of buttons. */
export function Segmented<T extends string>(props: {
  value: T;
  options: readonly (T | [T, string])[];
  onChange: (v: T) => void;
}) {
  return (
    <div class="segmented" role="radiogroup">
      {props.options.map((o) => {
        const [value, label] = Array.isArray(o) ? o : [o, o];
        return (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={props.value === value}
            class={props.value === value ? "on" : ""}
            onClick={() => props.onChange(value)}
          >
            {label}
          </button>
        );
      })}
    </div>
  );
}
