import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "./App";

describe("App", () => {
  it("renderiza la página de inicio", async () => {
    render(<App />);
    expect(
      await screen.findByRole("heading", { level: 1, name: "Seguros ERP" }),
    ).toBeInTheDocument();
  });
});
