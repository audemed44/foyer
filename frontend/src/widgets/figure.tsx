import type { ComponentChildren } from "preact";

/** A big number with a unit and a tracked label: the widgets' headline stat. */
export function Figure(props: {
  value: string;
  label: string;
  unit?: string;
  tone?: string;
  caption?: string;
  icon?: ComponentChildren;
}) {
  return (
    <div class={`figure ${props.tone ?? ""}`}>
      <p class="figure-value">
        {props.value}
        {props.unit && <span class="figure-unit">{props.unit}</span>}
      </p>
      <p class="eyebrow figure-label">
        {props.icon}
        {props.label}
      </p>
      {props.caption && <p class="figure-caption">{props.caption}</p>}
    </div>
  );
}
