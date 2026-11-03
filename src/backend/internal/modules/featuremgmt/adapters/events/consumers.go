// Package featuremgmtevents contiene los consumidores de eventos de featuremgmt.
package featuremgmtevents

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/app"
	"github.com/marvinrepremarca/seguros/internal/modules/featuremgmt/contracts"
	"github.com/marvinrepremarca/seguros/internal/platform/outbox"
)

// CacheInvalidation invalida las cachés de flags del tenant del evento en todas las instancias.
// Es la vía confiable (at-least-once) que complementa la invalidación inmediata tras el commit.
// Módulo núcleo: sin flag.
func CacheInvalidation(inv app.Invalidator) outbox.Consumer {
	return outbox.Consumer{
		Name:  "featuremgmt.cache-invalidation",
		Topic: contracts.TopicFeatureToggled,
		Handle: func(ctx context.Context, _ pgx.Tx, e outbox.Event) error {
			var ev contracts.FeatureToggled
			if err := e.Decode(&ev); err != nil {
				return err
			}
			if err := inv.Invalidate(ctx, e.TenantID); err != nil {
				return fmt.Errorf("featuremgmt: invalidando caché de %s: %w", ev.FlagKey, err)
			}
			return nil
		},
	}
}
