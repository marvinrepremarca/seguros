# 0011 — Política de licencias: ningún software con pago por uso
- Estado: Aceptado
- Fecha: 2026-10-30
- Decisores: Marvin Delgado (dueño del producto)
- Reemplaza parcialmente: 0010 (lista cerrada de licencias)

## Contexto
ADR 0010 admitía solo MIT/BSD/Apache/MPL/ISC/PostgreSQL. Al montar el frontend (ROADMAP 0.7), `isbot` —dependencia transitiva de TanStack Router— resultó tener licencia *Unlicense* (dominio público) y el chequeo de licencias falló, aunque usarla no cuesta nada ni impone obligaciones.

El dueño del producto aclaró la intención de la regla: **no depender de software por el que haya que pagar por uso, por usuario, por instancia o por superar un umbral**. La lista cerrada era un medio, no el fin; con ella se rechazan componentes gratuitos y sin obligaciones (dominio público, variantes permisivas menos comunes).

Existe una segunda fuerza que la lista también protegía: el producto es un SaaS propietario. Las licencias copyleft fuertes (GPL, y sobre todo AGPL) no cobran dinero, pero enlazadas en el binario o servidas en red pueden obligar a publicar el código del producto.

## Opciones consideradas
1. **Mantener la lista cerrada y excepciones por paquete.** Pros: control máximo. Contras: cada dependencia transitiva nueva con licencia rara bloquea el build; no refleja la intención del negocio.
2. **Permitir cualquier licencia sin costo, incluida copyleft.** Pros: máxima libertad. Contras: AGPL/GPL enlazadas pueden obligar a liberar el código propietario del SaaS: riesgo legal, no económico.
3. **Criterio por intención: sin pago por uso + sin obligaciones sobre el código propio.** Permisivas y dominio público se aceptan; copyleft fuerte solo como herramienta o servicio sin modificar; todo modelo de pago o de "fuente disponible" se rechaza.

## Decisión
Opción 3.

| Categoría | Licencias | Uso |
|---|---|---|
| Permisivas y dominio público | MIT, MIT-0, BSD-2/3-Clause, 0BSD, ISC, Apache-2.0, PostgreSQL, Zlib, BlueOak-1.0.0, Unlicense, CC0-1.0, Python-2.0, CC-BY-4.0 (solo datos, p. ej. `caniuse-lite`) | Permitidas en cualquier capa |
| Copyleft débil por archivo | MPL-2.0 | Permitida enlazada sin modificar los archivos de la librería |
| Copyleft fuerte | GPL, LGPL, AGPL | Solo herramientas o servicios ejecutados sin modificar (golangci-lint, Grafana, Loki, k6); nunca enlazadas en `cmd/*` ni empaquetadas en el frontend |
| Con pago o restricción comercial | BSL, SSPL, Elastic, Commons Clause, ediciones Pro/Enterprise, SaaS por consumo como dependencia obligatoria | Prohibidas |

Se mantienen las decisiones de 0010 sobre componentes concretos (Valkey en vez de Redis ≥7.4, OpenBao en vez de Vault, SeaweedFS en vez de MinIO, River OSS sin Pro). Docker Desktop sigue permitido en estaciones de desarrollo bajo el umbral de su licencia; el runtime de servidores usa Docker Engine (Apache-2.0).

Fuera de alcance: los costos de **infraestructura** (VPS, dominio, almacenamiento de backups fuera del servidor, envío de SMS/WhatsApp/email) no son licencias de software. Donde haya un servicio gestionado opcional se prefiere la alternativa autoalojada (SeaweedFS antes que R2; stack Grafana propio antes que Grafana Cloud) y el proveedor queda detrás de un puerto para poder cambiarlo.

## Consecuencias
- Los chequeos `make licenses` (go-licenses) y `pnpm licenses` usan la lista de la tabla; una licencia fuera de ella hace fallar el CI y requiere revisión humana.
- `ARCHITECTURE.md` §9, `CLAUDE.md` (Stack) y `.claude/rules/security.md` remiten a este ADR.
- Riesgo: una dependencia puede cambiar de licencia en una versión nueva (como Redis o MinIO). Mitigación: versiones fijadas por lockfile y el chequeo en cada PR.
