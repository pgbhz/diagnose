# Docker Notes

## Build
- `docker build --target telbot -t diagnose-telbot .`
- `docker build --target dashboard -t diagnose-dashboard .`
- Build context trimmed via `.dockerignore`.

## Run (docker compose)
- `docker compose up --build`
- Creates two containers: `diagnose-telbot` and `diagnose-dashboard`.
- Both share configs/assets via bind mounts so edits reflect immediately.

## Image contents
- **Telbot target**: distroless base with `/app/telbot`, `/app/configs`, `/app/assets`, `.env` copy, and default `ENTRYPOINT ["/app/telbot"]`.
- **Dashboard target**: Python 3.12 slim base with FastAPI requirements installed, `pyservice/` code, configs/assets, and `CMD uvicorn ...` on port `8000`.

## Tips
- Mount a new `.env` or pass `env_file` per service for secrets.
- For production, replace bind mounts with managed volumes or config stores.
- Future services (databases, workers) can be added directly to `docker-compose.yml`.
