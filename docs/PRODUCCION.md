# Producción en un VPS

Todo corre en un solo servidor con Docker Compose (`infra/production/compose.yaml`):

| Servicio | Función |
|---|---|
| `web` | Caddy: HTTPS automático con Let's Encrypt, sirve la PWA compilada y reenvía `/api/*` a la API. Único servicio con puertos públicos (80 y 443). |
| `api` | API en Go. No expone puertos; confía en `X-Forwarded-For` solo desde la red interna. |
| `migrate` | Aplica las migraciones antes de cada arranque de la API. |
| `db` | PostgreSQL 17 en un volumen de Docker, sin puertos publicados. |
| `backup` | `pg_dump` diario en `/var/backups/retail`, con su SHA-256, conservando 14 días. |

Web y API comparten origen, así que la cookie `retail_session` sigue siendo `Secure`, `HttpOnly` y `SameSite=Strict`.

## Preparación del servidor (una sola vez)

1. Ubuntu 24.04 con Docker y acceso SSH por clave.
2. Firewall: `ufw allow OpenSSH && ufw allow 80/tcp && ufw allow 443 && ufw enable`.
3. SSH solo por clave: `PasswordAuthentication no` en `/etc/ssh/sshd_config.d/`.
4. Crear `/opt/retail/app/infra/production/.env` (permisos 600) a partir de `.env.example`, con una contraseña de base aleatoria generada en el servidor (`openssl rand -hex 32`). El archivo nunca sale del servidor ni se commitea.

## Desplegar

```sh
DEPLOY_HOST=root@<ip> infra/production/deploy.sh
```

Copia el `HEAD` commiteado (se niega si hay cambios sin commit), construye las imágenes en el servidor, aplica migraciones, reinicia y verifica `/api/health/ready`. Para volver atrás, se despliega el commit anterior. Las migraciones no se revierten solas.

## Alta de un comercio

```sh
ssh root@<ip>
cd /opt/retail/app/infra/production
docker compose run --rm -T --no-deps migrate provision-tenant \
  --slug <slug> --name "<Comercio>" --timezone America/Argentina/Buenos_Aires --currency ARS \
  --store-code CEN --store-name "<Sucursal>" --owner-email <correo> --owner-name "<Nombre>"
```

La contraseña inicial se escribe por la entrada estándar (no queda en el historial ni en los procesos).

## Respaldos y restauración

- Diario, en `/var/backups/retail/retail-<fecha>.dump` con su `.sha256`. Conviene además el respaldo semanal del proveedor y copiar los dumps fuera del servidor.
- Restaurar sobre la base vacía:
  ```sh
  docker compose stop api web
  docker compose exec -T db pg_restore -U retail -d retail --clean --if-exists < /var/backups/retail/<archivo>.dump
  docker compose start api web
  ```

## Dominio propio

Apuntar el registro A del dominio a la IP del servidor, cambiar `DOMAIN` en `.env` y volver a desplegar. Caddy obtiene el certificado nuevo solo. Las sesiones abiertas se pierden porque cambia el origen.
