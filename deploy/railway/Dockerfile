# The listmonk service of this fork's Railway project (the "Deploy on Railway" template in docs/docs/content/installation.md).
FROM listmonk/listmonk:v6.2.0
# Railway gives the port in $PORT; the database comes from the Postgres service's reference variables (LISTMONK_db__*).
CMD ["sh", "-c", "export LISTMONK_app__address=0.0.0.0:${PORT:-9000} && ./listmonk --install --idempotent --yes --config '' && ./listmonk --upgrade --yes --config '' && exec ./listmonk --config ''"]
