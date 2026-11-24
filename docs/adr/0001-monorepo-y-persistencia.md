# ADR 0001 — Monorepo y persistencia

La API y la web comparten repositorio, contrato OpenAPI y CI. La API es un monolito modular Go/Gin. Los módulos contienen la lógica de aplicación y sus consultas acotadas. El transporte y la configuración se componen en la capa de plataforma. pgx y SQL explícito permiten revisar predicados de aislamiento y transacciones junto con el código. Las consultas iniciales son pocas y el cliente web tiene tipos TypeScript escritos a partir del contrato.

PostgreSQL 17 es el motor local y de integración. Un comando explícito aplica el SQL embebido, en una transacción protegida por un advisory lock. Registra el checksum de cada archivo aplicado. Para cambios posteriores se agregan migraciones que avanzan el esquema.

La PWA usa Vite y React con un único origen para web y API. El service worker cachea solamente JS y CSS versionados del frontend. Las páginas y solicitudes de API se resuelven por red.
