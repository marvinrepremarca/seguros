import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { useState } from "react";

import { createQueryClient } from "./query-client";
import { createAppRouter } from "./router";

interface AppProps {
  queryClient?: QueryClient;
}

export function App({ queryClient: injected }: AppProps) {
  const [queryClient] = useState(() => injected ?? createQueryClient());
  const [router] = useState(() => createAppRouter(queryClient));
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
