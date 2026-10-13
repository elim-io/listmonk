#!/bin/sh
# Cloud Run job command: install the schema on an empty database, then run the
# pending upgrades. Run it before each deploy of the service, which does not
# migrate on start.
set -e

# --install prints the address to open at the end, and fails when there is none.
export LISTMONK_app__address="0.0.0.0:${PORT:-8080}"

./listmonk --install --idempotent --yes --config ''
exec ./listmonk --upgrade --yes --config ''
