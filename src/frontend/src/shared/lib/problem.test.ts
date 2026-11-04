import { describe, expect, it } from "vitest";

import { ApiError } from "./api-client";
import { problemMessage } from "./problem";

describe("problemMessage", () => {
  it("no hay mensaje sin error", () => {
    expect(problemMessage(null)).toBeNull();
    expect(problemMessage(undefined)).toBeNull();
  });

  it("traduce los códigos conocidos", () => {
    const err = new ApiError(503, { type: "policies.issue.compliance_unavailable" });
    expect(problemMessage(err)).toContain("SARLAFT");
    expect(problemMessage(err)).toContain("no se expidió");
  });

  it("usa un mensaje por estado para códigos desconocidos", () => {
    expect(problemMessage(new ApiError(403, { type: "x.y" }))).toContain("permiso");
    expect(problemMessage(new ApiError(404, undefined))).toContain("No se encontró");
  });

  it("nunca muestra detalles internos", () => {
    const msg = problemMessage(
      new ApiError(500, { type: "platform.internal", detail: "SELECT secreto" }),
    );
    expect(msg).not.toContain("SELECT");
    expect(problemMessage(new Error("pgx: conexión rechazada"))).not.toContain("pgx");
  });
});
