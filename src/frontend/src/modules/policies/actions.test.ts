import { describe, expect, it } from "vitest";

import { availableActions } from "./actions";

const all = { manage: true, cancel: true, endorse: true, assign: true };

describe("availableActions", () => {
  it.each([
    ["in_quotation", ["edit", "accept", "reassign"]],
    ["in_process", ["edit", "issue", "reject", "reassign"]],
    ["active", ["endorse", "cancel", "reassign", "renew"]],
    ["in_arrears", ["endorse", "cancel", "reassign", "renew", "reinstate"]],
    ["cancelled", []],
    ["expired", ["reassign", "renew"]],
    ["renewed", []],
    ["not_renewed", []],
    ["rejected", []],
  ] as const)("%s", (status, expected) => {
    expect(availableActions(status, all)).toEqual(expected);
  });

  it("filtra por permisos", () => {
    const none = { manage: false, cancel: false, endorse: false, assign: false };
    expect(availableActions("in_process", none)).toEqual([]);
    expect(availableActions("active", { ...none, cancel: true })).toEqual(["cancel"]);
    expect(availableActions("active", { ...none, endorse: true })).toEqual(["endorse"]);
    expect(availableActions("expired", { ...none, assign: true })).toEqual(["reassign"]);
  });
});
