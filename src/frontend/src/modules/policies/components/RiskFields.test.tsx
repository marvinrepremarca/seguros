import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { RiskFields } from "./RiskFields";

describe("RiskFields", () => {
  it("rotula, marca obligatorios y avisa los cambios", async () => {
    const onChange = vi.fn();
    render(
      <RiskFields
        fields={[
          { name: "plate", label: "Placa", type: "string", required: true },
          { name: "use", label: "Uso", type: "string", required: false },
        ]}
        values={{}}
        onChange={onChange}
      />,
    );
    expect(screen.getByLabelText("Placa *")).toBeRequired();
    expect(screen.getByLabelText("Uso")).not.toBeRequired();
    await userEvent.type(screen.getByLabelText("Uso"), "P");
    expect(onChange).toHaveBeenCalledWith("use", "P");
  });
});
