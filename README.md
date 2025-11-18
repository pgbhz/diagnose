# Diagnose – Sistema de Teleodontologia

Diagnose automatiza a triagem inicial de casos clínicos via Telegram (bot em Go) e agora também expõe um painel web em FastAPI para acompanhar os diagnósticos registrados em `configs/diagnosis.json`.

## Serviços incluídos

- **telbot (Go)** – escuta o Telegram, conduz conversas pré-definidas e persiste os diagnósticos com base nos arquivos de configuração existentes.
- **Diagnosis Dashboard (FastAPI)** – interface protegida por Basic Auth que lista todos os pacientes encontrados em `diagnosis.json` e permite inspecionar o histórico completo de cada pessoa.
- **Redis queue** – transporta, de forma bem simples, os `chat_id` recém processados pelo bot para que o dashboard exiba alertas em tempo real.

As credenciais usadas pelo painel são lidas de `configs/auth.json`, porém apenas entradas dentro de `superusers` (ex.: médicos) podem se autenticar; a tabela `users` continua existindo para o bot, mas não concede acesso ao dashboard.

## Desenvolvimento local

### Pré-requisitos

- Go 1.20+
- Python 3.11+ (recomendado criar um ambiente virtual)
- Telegram bot token disponível via `TELEGRAM_TOKEN`

### Executar o bot (Go)

```bash
cd src
go run .
```

Certifique-se de preencher `.env` ou exportar as variáveis exigidas (`TELEGRAM_TOKEN`, `ASSETS_DIR`, etc.).

### Executar o painel FastAPI

```bash
python -m venv .venv
source .venv/bin/activate
pip install -r pyservice/requirements.txt

export CONFIG_DIR=${CONFIG_DIR:-$(pwd)/src/configs}
export ASSETS_DIR=${ASSETS_DIR:-$(pwd)/src/assets}
export REDIS_URL=${REDIS_URL:-redis://localhost:6379/0}
uvicorn pyservice.app.main:app --reload --port 8000
```

Autentique-se com um usuário presente na lista `superusers` de `configs/auth.json`. Cada linha da tabela principal leva à página de detalhes do paciente correspondente.

O bot publica cada `chat_id` processado em `CHAT_EVENT_QUEUE` (default `diagnosis:chat_events`) via Redis. Configure localmente com:

```bash
export REDIS_ADDR=localhost:6379
export CHAT_EVENT_QUEUE=diagnosis:chat_events
```

## Testes

- Bot (Go):

	```bash
	cd src
	go test ./...
	```

- Painel (Python):

	```bash
	python -m pytest pyservice/tests
	```

## Docker e Compose

O `Dockerfile` expõe dois alvos:

- `telbot` – imagem minimalista com o binário Go.
- `dashboard` – imagem Python com o FastAPI/uvicorn.

Você pode construir manualmente cada serviço:

```bash
docker build -t diagnose-telbot --target telbot .
docker build -t diagnose-dashboard --target dashboard .
```

Para desenvolver localmente com ambos os serviços (mais o Redis embutido), use o `docker-compose.yml`:

```bash
docker compose up --build
```

Por padrão o painel é exposto em `http://localhost:8000`. Ambos os contêineres compartilham `src/configs` e `src/assets` via bind mounts; ajuste ou converta para volumes conforme o ambiente de nuvem escolhido.
