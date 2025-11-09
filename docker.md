# Docker Notes

## Build
- `docker build -t telbot .`
- Build context trimmed via `.dockerignore`.

## Run
- Local dev (uses baked-in `.env`): `docker run --rm --name telbot telbot`
- Preferred for secrets: `docker run --rm --name telbot --env-file src/.env telbot`

## Contents
- Binary compiled from `src/main.go`.
- Copies `src/configs/`, `src/states.json`, and `.env` into `/app`.

## Tips
- Update `.env` or mount a new one if tokens change.
- Rebuild after modifying Go code or runtime assets.
