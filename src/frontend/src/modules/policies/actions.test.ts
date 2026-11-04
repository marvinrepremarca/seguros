import { describe, expect, it } from "vitest";

import { availableActions } from "./actions";

const all = { manage: true, cancel: true, endorse: true };

describe("availableActions", () => {
  it.each([
    ["in_quotation", ["edit", "accept"]],
    ["in_process", ["edit", "issue", "reject"]],
    ["active", ["endorse", "cancel"]],
    ["in_arrears", ["endorse", "cancel"]],
    ["cancelled", []],
    ["expired", []],
    ["renewed", []],
    ["not_renewed", []],
    ["rejected", []],
  ] as const)("%s", (status, expected) => {
    expect(availableActions(status, all)).toEqual(expected);
  });

  it("filtra por permisos", () => {
    const none = { manage: false, cancel: false, endorse: false };
    expect(availableActions("in_process", none)).toEqual([]);
    expect(availableActions("active", { ...none, cancel: true })).toEqual(["cancel"]);
    expect(availableActions("active", { ...none, endorse: true })).toEqual(["endorse"]);
  });
});
