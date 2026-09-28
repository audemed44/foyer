import { describe, expect, it } from "vitest";
import { parseAnsi, stripAnsi } from "./ansi";

describe("parseAnsi", () => {
  it("passes plain text through", () => {
    expect(parseAnsi("hello")).toEqual([{ text: "hello" }]);
  });

  it("applies and resets colours and weight", () => {
    const segs = parseAnsi("a \x1b[1;31mERROR\x1b[0m b");
    expect(segs).toEqual([
      { text: "a " },
      { text: "ERROR", bold: true, fg: "#ff5f56" },
      { text: " b" },
    ]);
  });

  it("handles bright, 256 and true colour", () => {
    expect(parseAnsi("\x1b[92mok")[0].fg).toBe("#56d364");
    expect(parseAnsi("\x1b[38;5;196mx")[0].fg).toBe("rgb(255 0 0)");
    expect(parseAnsi("\x1b[38;2;1;2;3mx")[0].fg).toBe("rgb(1 2 3)");
  });

  it("ignores backgrounds and drops non-colour escapes", () => {
    const segs = parseAnsi("\x1b[48;5;21m\x1b[2Kx\x1b]0;title\x07y");
    expect(segs.map((s) => s.text).join("")).toBe("xy");
    expect(segs[0].fg).toBeUndefined();
  });

  it("strips escapes to plain text", () => {
    expect(stripAnsi("\x1b[32mINFO\x1b[39m started")).toBe("INFO started");
  });
});
