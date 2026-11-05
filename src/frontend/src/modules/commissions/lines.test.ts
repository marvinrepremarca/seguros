import { describe, expect, it } from "vitest";

import { parseLines } from "./lines";

describe("parseLines", () => {
  it("lee una línea por póliza e ignora las vacías, sin convertir el valor a número", () => {
    expect(parseLines("P-1;1000.50\n\n P-2 ; 20 \n")).toEqual([
      { policyNumber: "P-1", amount: "1000.50" },
      { policyNumber: "P-2", amount: "20" },
    ]);
  });
});
