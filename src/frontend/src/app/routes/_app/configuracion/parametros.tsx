import { createFileRoute, notFound } from "@tanstack/react-router";

import { BillingSettings } from "@/modules/billing";
import { CommissionSettingsForm } from "@/modules/commissions";
import { RenewalRulesForm } from "@/modules/policies";
import { RenewalSettings } from "@/modules/renewals";
import { features } from "@/shared/lib/features";
import { FeatureGate } from "@/shared/lib/providers";
import { hasPermission, permissions, usePermission } from "@/shared/lib/session";

export const Route = createFileRoute("/_app/configuracion/parametros")({
  beforeLoad: ({ context }) => {
    // Solo UX: el backend exige el permiso en cada operación.
    const allowed = [
      permissions.policySettingsManage,
      permissions.billingSettingsManage,
      permissions.renewalSettingsManage,
      permissions.commissionRateManage,
    ].some((p) => hasPermission(context.session, p));
    if (!allowed) {
      // eslint-disable-next-line @typescript-eslint/only-throw-error -- API de TanStack Router
      throw notFound();
    }
  },
  component: Page,
});

function Page() {
  const canPolicies = usePermission(permissions.policySettingsManage);
  const canBilling = usePermission(permissions.billingSettingsManage);
  const canRenewals = usePermission(permissions.renewalSettingsManage);
  const canCommissions = usePermission(permissions.commissionRateManage);
  return (
    <section aria-labelledby="parameters-title" className="space-y-6">
      <h1 id="parameters-title" className="text-2xl font-semibold">
        Parámetros
      </h1>
      {canPolicies && (
        <div className="space-y-2">
          <h2 className="text-lg font-medium">Pólizas</h2>
          <RenewalRulesForm />
        </div>
      )}
      {canBilling && (
        <FeatureGate flag={features.billing.module}>
          <div className="space-y-2">
            <h2 className="text-lg font-medium">Cartera</h2>
            <BillingSettings />
          </div>
        </FeatureGate>
      )}
      {canRenewals && (
        <FeatureGate flag={features.renewals.module}>
          <div className="space-y-2">
            <h2 className="text-lg font-medium">Renovaciones</h2>
            <RenewalSettings />
          </div>
        </FeatureGate>
      )}
      {canCommissions && (
        <FeatureGate flag={features.commissions.module}>
          <div className="space-y-2">
            <h2 className="text-lg font-medium">Comisiones</h2>
            <CommissionSettingsForm />
          </div>
        </FeatureGate>
      )}
    </section>
  );
}
