# Política de seguridad

## Reportar una vulnerabilidad

No abras un issue público. Usá **Report a vulnerability** en la pestaña **Security** del repositorio (reporte privado de GitHub) e incluí:

- qué componente afecta (API, web, migraciones, infraestructura);
- los pasos para reproducirlo y su impacto;
- la versión o el commit probado.

Respondemos dentro de los 7 días. Pedimos no divulgar el problema hasta que exista una corrección publicada.

## Alcance

Entran en el alcance la API en Go (`apps/api`), la PWA (`apps/web`), las migraciones SQL y la configuración de `infra/` y `compose.yaml`.

Quedan fuera:
- las credenciales de ejemplo de `.env.example`, `compose.yaml` y la CI, que solo sirven para entornos locales;
- los despliegues que no sigan la lista de verificación de producción del [README](README.md#antes-de-desplegar-en-producción).

## Versiones con soporte

Solo la rama `main` recibe correcciones de seguridad.
