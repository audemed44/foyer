/**
 * Turns ANSI-coloured log text into styled segments. Handles the SGR codes
 * containers actually emit (bold, dim, italic, underline, 16/256/true colour)
 * and strips every other escape sequence.
 */
export type Segment = {
  text: string;
  fg?: string;
  bold?: boolean;
  dim?: boolean;
  italic?: boolean;
  underline?: boolean;
};

// A palette that reads on both black and light backgrounds.
const BASIC = [
  "#6b7280",
  "#ff5f56",
  "#27c93f",
  "#f5c542",
  "#4f8bff",
  "#c678dd",
  "#2ec4c4",
  "#d4d4d8",
  "#9ca3af",
  "#ff7b72",
  "#56d364",
  "#f8d866",
  "#79a8ff",
  "#d2a8ff",
  "#56d4dd",
  "#f4f4f5",
];

function xterm256(n: number): string | undefined {
  if (n < 16) return BASIC[n];
  if (n < 232) {
    const i = n - 16;
    const level = (v: number) => (v === 0 ? 0 : 55 + v * 40);
    return `rgb(${level(Math.floor(i / 36))} ${level(Math.floor(i / 6) % 6)} ${level(i % 6)})`;
  }
  if (n < 256) {
    const g = 8 + (n - 232) * 10;
    return `rgb(${g} ${g} ${g})`;
  }
  return undefined;
}

// eslint-disable-next-line no-control-regex
const ESCAPE = /\x1b\[([0-9;]*)([A-Za-z])|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]/g;

export function parseAnsi(input: string): Segment[] {
  const out: Segment[] = [];
  let style: Omit<Segment, "text"> = {};
  let last = 0;
  const push = (text: string) => {
    if (text) out.push({ text, ...style });
  };
  for (const match of input.matchAll(ESCAPE)) {
    push(input.slice(last, match.index));
    last = match.index! + match[0].length;
    if (match[2] !== "m") continue; // not a colour code: drop it
    const codes = (match[1] || "0").split(";").map(Number);
    for (let i = 0; i < codes.length; i++) {
      const c = codes[i];
      if (c === 0) style = {};
      else if (c === 1) style = { ...style, bold: true };
      else if (c === 2) style = { ...style, dim: true };
      else if (c === 3) style = { ...style, italic: true };
      else if (c === 4) style = { ...style, underline: true };
      else if (c === 22) style = { ...style, bold: false, dim: false };
      else if (c === 23) style = { ...style, italic: false };
      else if (c === 24) style = { ...style, underline: false };
      else if (c >= 30 && c <= 37) style = { ...style, fg: BASIC[c - 30] };
      else if (c >= 90 && c <= 97) style = { ...style, fg: BASIC[c - 90 + 8] };
      else if (c === 39) style = { ...style, fg: undefined };
      else if (c === 38 && codes[i + 1] === 5) {
        style = { ...style, fg: xterm256(codes[i + 2]) };
        i += 2;
      } else if (c === 38 && codes[i + 1] === 2) {
        style = { ...style, fg: `rgb(${codes[i + 2]} ${codes[i + 3]} ${codes[i + 4]})` };
        i += 4;
      } else if (c === 48) {
        i += codes[i + 1] === 5 ? 2 : codes[i + 1] === 2 ? 4 : 0; // backgrounds are ignored
      }
    }
  }
  push(input.slice(last));
  return out;
}

export function stripAnsi(input: string): string {
  return input.replace(ESCAPE, "");
}
