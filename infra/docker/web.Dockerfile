FROM node:22-bookworm-slim AS build
WORKDIR /app
COPY apps/web/package.json apps/web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY apps/web/ ./
RUN npm run build

FROM caddy:2.10-alpine
COPY infra/production/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /app/dist /srv
