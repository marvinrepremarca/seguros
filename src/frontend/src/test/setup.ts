import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, beforeEach, vi } from "vitest";

import { server } from "./msw";

beforeAll(() => {
  server.listen({ onUnhandledFrame: "error" });
});

beforeEach(() => {
  // jsdom no implementa scrollTo; el router lo usa para restaurar el scroll.
  vi.spyOn(window, "scrollTo").mockImplementation(() => undefined);
});

afterEach(() => {
  cleanup();
  server.resetHandlers();
  window.history.replaceState(null, "", "/");
});

afterAll(() => {
  server.close();
});
