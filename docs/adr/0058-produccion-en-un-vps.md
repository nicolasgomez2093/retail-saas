# ADR 0058 — Producción en un VPS con Docker Compose

## Contexto

El piloto necesita una dirección pública con HTTPS. La sesión exige que la web y la API compartan origen (`SameSite=Strict`, verificación de `Origin`). Un hosting estático no puede correr la API ni PostgreSQL, y repartir web, API y base entre proveedores agrega orígenes cruzados, latencia y cuentas que mantener.

## Decisión

- Un VPS (Hostinger KVM 2, Ubuntu 24.04) con Docker Compose: Caddy, API, migraciones, PostgreSQL 17 y un servicio de respaldo.
- Caddy termina HTTPS (Let's Encrypt), sirve la PWA compilada con caída a `index.html` y reenvía `/api/*`. Agrega CSP, HSTS y cabeceras de caché para la web; la API conserva las suyas.
- Solo los puertos 22, 80 y 443 quedan abiertos. La base y la API no publican puertos.
- `TRUSTED_PROXIES` se limita a las subredes internas fijas de Compose (IPv4 e IPv6), así el límite de intentos de inicio de sesión ve la IP real. La red interna tiene IPv6 para que los clientes IPv6 no lleguen todos con la dirección del proxy de Docker.
- Las migraciones corren en cada despliegue antes de la API.
- Respaldo lógico diario con SHA-256 y retención de 14 días, en el disco del servidor.
- Despliegue con `infra/production/deploy.sh`: copia el commit, construye en el servidor y verifica salud. Los secretos viven solo en el `.env` del servidor.
- Mientras no haya dominio propio se usa el nombre que asigna el proveedor (`srvNNNN.hstgr.cloud`).

## Consecuencias

- Un único servidor es un único punto de falla; aceptable para el piloto. Los respaldos deben copiarse también fuera del servidor.
- Las actualizaciones del sistema operativo y de las imágenes base quedan a cargo nuestro.
- Cambiar al dominio propio cambia el origen y cierra las sesiones abiertas.
