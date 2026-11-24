# factory-api

Go-Build-Worker (geplant): `POST /api/builds` mit `{name, target:{goos,goarch}, modules[], defaults:{region}}`, Log-Stream per SSE, Auslieferung als `.tar.gz`. Baut ausschließlich mit `CGO_ENABLED=0`.
