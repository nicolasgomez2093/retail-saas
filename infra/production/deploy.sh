#!/bin/bash
# Deploys the committed HEAD to the VPS: copies the source, builds the images there and restarts the stack.
# Usage: DEPLOY_HOST=root@203.0.113.10 infra/production/deploy.sh
set -euo pipefail
host=${DEPLOY_HOST:?set DEPLOY_HOST, for example root@203.0.113.10}
root=$(git rev-parse --show-toplevel)
cd "$root"
if [ -n "$(git status --porcelain)" ]; then
  echo "Hay cambios sin commit: se despliega solo lo commiteado." >&2
  exit 1
fi
revision=$(git rev-parse --short HEAD)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git archive HEAD | tar -x -C "$work"
echo "$revision" > "$work/REVISION"
ssh "$host" 'test -f /opt/retail/app/infra/production/.env' || {
  echo "Falta /opt/retail/app/infra/production/.env en el servidor (ver docs/PRODUCCION.md)." >&2
  exit 1
}
rsync -az --delete --exclude /infra/production/.env "$work"/ "$host:/opt/retail/app/"
ssh "$host" 'cd /opt/retail/app/infra/production && docker compose up -d --build --wait --remove-orphans && docker image prune -f >/dev/null'
ssh "$host" 'curl -fsS --max-time 10 https://$(grep ^DOMAIN= /opt/retail/app/infra/production/.env | cut -d= -f2)/api/health/ready'
echo
echo "Desplegado $revision"
