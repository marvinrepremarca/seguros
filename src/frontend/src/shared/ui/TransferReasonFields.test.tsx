import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { TRANSFER_REASONS } from "@/shared/lib/transferReasons";

import { TransferReasonFields } from "./TransferReasonFields";

describe("TransferReasonFields", () => {
  it("ofrece todo el catálogo y exige nota solo con Otro", () => {
    const props = { note: "", onReasonChange: vi.fn(), onNoteChange: vi.fn() };
    const { rerender } = render(<TransferReasonFields {...props} reason="role_change" />);
    expect(screen.getAllByRole("option")).toHaveLength(TRANSFER_REASONS.length + 1);
    expect(screen.getByLabelText("Nota (opcional)")).not.toBeRequired();
    rerender(<TransferReasonFields {...props} reason="other" />);
    expect(screen.getByLabelText(/^Nota/)).toBeRequired();
  });
});
