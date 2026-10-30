import { describe, expect, it } from "vitest";

import { cn } from "./cn";

describe("cn", () => {
  it("resuelve conflictos de Tailwind a favor de la última clase", () => {
    expect(cn("px-2 py-1", "px-4")).toBe("py-1 px-4");
  });

  it("omite valores falsy", () => {
    expect(cn("a", false, undefined, null, "b")).toBe("a b");
  });
});
