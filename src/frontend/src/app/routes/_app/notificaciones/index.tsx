import { createFileRoute, notFound } from "@tanstack/react-router";

import { NotificationsPage } from "@/modules/notifications";
import { features, isFeatureEnabled } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/notificaciones/")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige flag y permiso en cada operación.
    if (
      !isFeatureEnabled(context.features, features.notifications.module) ||
      !hasPermission(context.session, permissions.notificationRead)
    ) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: () => (
    <FeatureGate flag={features.notifications.module}>
      <NotificationsPage />
    </FeatureGate>
  ),
});
