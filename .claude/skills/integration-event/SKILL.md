---
name: integration-event
description: Define, publica y consume eventos de integración entre módulos Go usando Watermill con outbox transaccional en PostgreSQL, inbox idempotente y guardia de feature flag. Úsala cuando un módulo deba reaccionar a algo ocurrido en otro.
---
# Eventos de integración entre módulos

## Definir (módulo publicador)
- En `internal/modules/<pub>/contracts/events.go`:
  ```go
  type PolicyIssuedV1 struct {
      EventID    uuid.UUID `json:"event_id"`
      TenantID   uuid.UUID `json:"tenant_id"`
      OccurredAt time.Time `json:"occurred_at"`
      PolicyID   uuid.UUID `json:"policy_id"`
      // ...
  }
  const TopicPolicyIssuedV1 = "policies.policy_issued.v1"
  ```
- Nombre en pasado, versión en nombre y topic. Cambio incompatible ⇒ `V2` y convivencia temporal.
- Solo IDs y datos no Restringidos. Montos como string decimal + moneda.
- Registra el evento en DOMAIN.md §3.

## Publicar
- Dentro de `db.WithTenantTx`, con el publisher de outbox ligado a la misma `pgx.Tx` (`outbox.Publish(ctx, tx, topic, evt)`). Nunca publicar tras el commit ni directo al broker.

## Consumir (módulo suscriptor)
- `internal/modules/<sub>/app/on_<hecho>.go`, registrado en `module.go` → `Subscribers()`.
- Orden obligatorio: 1) poner `TenantID` del evento en `ctx`, 2) `flags.Enabled` del módulo suscriptor → si OFF, `return nil`, 3) inbox: `INSERT ... ON CONFLICT DO NOTHING` por `(consumer, event_id)` en la misma tx; si ya existía, `return nil`, 4) lógica.
- Errores de infraestructura se devuelven (Watermill reintenta con backoff); tras N intentos → poison queue + alerta.

## Pruebas
- Publicador: el evento queda en la tabla outbox tras el comando.
- Consumidor: entrega duplicada procesa una sola vez; sin efectos con flag OFF; tenant correcto.
