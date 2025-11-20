import asyncio
import contextlib
import json
import logging
import os
import secrets
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, AsyncGenerator, Dict, List, Optional

import httpx
from fastapi import Depends, FastAPI, HTTPException, Request, status
from fastapi.responses import HTMLResponse, RedirectResponse, StreamingResponse
from fastapi.security import HTTPBasic, HTTPBasicCredentials
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from redis import asyncio as aioredis
from pydantic import BaseModel

SERVICE_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_CONFIG_DIR = SERVICE_ROOT.parent / "src" / "configs"
DEFAULT_ASSETS_DIR = SERVICE_ROOT.parent / "src" / "assets"

CONFIG_DIR = Path(os.getenv("CONFIG_DIR", DEFAULT_CONFIG_DIR))
ASSETS_DIR = Path(os.getenv("ASSETS_DIR", DEFAULT_ASSETS_DIR))
FASTAPI_TITLE = os.getenv("FASTAPI_TITLE", "Diagnosis Dashboard")
REDIS_URL = os.getenv("REDIS_URL", "redis://localhost:6379/0")
CHAT_EVENT_QUEUE = os.getenv("CHAT_EVENT_QUEUE", "diagnosis:chat_events")
MODEL_SERVICE_URL = os.getenv("MODEL_SERVICE_URL", "http://localhost:8080")

app = FastAPI(title=FASTAPI_TITLE, version="0.1.0")
app.mount(
    "/assets", StaticFiles(directory=str(ASSETS_DIR), check_dir=False), name="assets"
)

TEMPLATES = Jinja2Templates(directory=str(SERVICE_ROOT / "templates"))
SECURITY = HTTPBasic()
logger = logging.getLogger("diagnosis_dashboard")


class AuthStore:
    """Loads credentials from auth.json on demand."""

    def __init__(self, config_dir: Path) -> None:
        self.config_dir = config_dir

    @property
    def path(self) -> Path:
        return self.config_dir / "auth.json"

    def read_credentials(self, section: str = "users") -> Dict[str, str]:
        try:
            data = _load_json(self.path)
        except FileNotFoundError:
            raise HTTPException(
                status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
                detail="auth.json file is missing",
            ) from None

        records = data.get(section, [])
        credentials = {
            item.get("username", ""): item.get("password", "")
            for item in records
            if item.get("username")
        }
        return credentials


class DiagnosisStore:
    """Loads diagnosis history from diagnosis.json."""

    def __init__(self, config_dir: Path) -> None:
        self.config_dir = config_dir
        self._cache: Optional[Dict[str, List[Dict[str, Any]]]] = None

    @property
    def path(self) -> Path:
        return self.config_dir / "diagnosis.json"

    def read_entries(self) -> Dict[str, List[Dict[str, Any]]]:
        if self._cache is None:
            self._cache = self._load()
        return self._cache

    def reload(self) -> Dict[str, List[Dict[str, Any]]]:
        self._cache = None
        return self.read_entries()

    def _load(self) -> Dict[str, List[Dict[str, Any]]]:
        try:
            data = _load_json(self.path)
        except FileNotFoundError:
            return {}

        return {
            str(patient): list(entries)
            for patient, entries in data.items()
            if isinstance(entries, list)
        }


auth_store = AuthStore(CONFIG_DIR)
diagnosis_store = DiagnosisStore(CONFIG_DIR)


class EventBroadcaster:
    """Tracks active SSE subscribers and broadcasts queue events."""

    def __init__(self) -> None:
        self._queues: List[asyncio.Queue] = []
        self._lock = asyncio.Lock()

    async def register(self) -> asyncio.Queue:
        queue: asyncio.Queue = asyncio.Queue()
        async with self._lock:
            self._queues.append(queue)
        return queue

    async def unregister(self, queue: asyncio.Queue) -> None:
        async with self._lock:
            if queue in self._queues:
                self._queues.remove(queue)

    async def broadcast(self, payload: Dict[str, Any]) -> None:
        async with self._lock:
            queues = list(self._queues)
        for queue in queues:
            await queue.put(payload)


broadcaster = EventBroadcaster()


class ClassifyPayload(BaseModel):
    photo_path: str


class TrainPayload(BaseModel):
    epochs: int | None = None
    patience: int | None = None
    learning_rate: float | None = None
    batch_size: int | None = None
    augment: bool | None = None


async def _connect_redis() -> aioredis.Redis:
    client = aioredis.from_url(
        REDIS_URL,
        encoding="utf-8",
        decode_responses=True,
    )
    await client.ping()
    return client


async def _queue_worker(redis_client: aioredis.Redis) -> None:
    while True:
        try:
            entry = await redis_client.blpop(CHAT_EVENT_QUEUE)
            if not entry:
                continue
            _, chat_id = entry
            payload = {
                "chat_id": chat_id,
                "received_at": datetime.now(timezone.utc).isoformat(),
            }
            await broadcaster.broadcast(payload)
        except asyncio.CancelledError:
            break
        except Exception as exc:  # pragma: no cover - background diagnostics
            logger.warning("Queue listener error: %s", exc)
            await asyncio.sleep(2)


async def _startup_queue_listener() -> None:
    try:
        redis_client = await _connect_redis()
    except Exception as exc:  # pragma: no cover - startup diagnostics
        logger.warning("Redis unavailable: %s", exc)
        app.state.redis = None
        app.state.queue_task = None
        return

    app.state.redis = redis_client
    app.state.queue_task = asyncio.create_task(_queue_worker(redis_client))


async def _shutdown_queue_listener() -> None:
    queue_task = getattr(app.state, "queue_task", None)
    if queue_task:
        queue_task.cancel()
        with contextlib.suppress(asyncio.CancelledError):
            await queue_task

    redis_client = getattr(app.state, "redis", None)
    if redis_client:
        await redis_client.close()


@app.on_event("startup")
async def _startup_event() -> None:
    app.state.redis = None
    app.state.queue_task = None
    await _startup_queue_listener()


@app.on_event("shutdown")
async def _shutdown_event() -> None:
    await _shutdown_queue_listener()


def _load_json(path: Path) -> Any:
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def _parse_timestamp(value: Optional[str]) -> Optional[datetime]:
    if not value:
        return None
    sanitized = value.replace("Z", "+00:00") if value.endswith("Z") else value
    try:
        dt = datetime.fromisoformat(sanitized)
        if dt.tzinfo is None:
            dt = dt.replace(tzinfo=timezone.utc)
        return dt
    except ValueError:
        return None


def _format_timestamp(dt: Optional[datetime]) -> str:
    if not dt:
        return "Unknown"
    return dt.astimezone().strftime("%Y-%m-%d %H:%M %Z")


def _format_verdict(value: Any) -> str:
    if isinstance(value, bool):
        return "Positive" if value else "Negative"
    if isinstance(value, str) and value:
        return value.capitalize()
    return "Unknown"


def _photo_url(photo_path: Optional[str]) -> Optional[str]:
    if not photo_path:
        return None
    filename = Path(photo_path).name
    if not filename:
        return None
    return f"/assets/{filename}"


def _normalize_entries(entries: List[Dict[str, Any]]) -> List[Dict[str, Any]]:
    normalized = []
    for entry in entries:
        timestamp = _parse_timestamp(entry.get("timestamp"))
        sort_key = timestamp.timestamp() if timestamp else float("-inf")
        normalized.append(
            {
                "timestamp": _format_timestamp(timestamp),
                "verdict": _format_verdict(entry.get("verdict")),
                "rationale": entry.get("rationale", ""),
                "photo_path": entry.get("photo_path"),
                "photo_url": _photo_url(entry.get("photo_path")),
                "sort_key": sort_key,
            }
        )
    normalized.sort(key=lambda item: item["sort_key"], reverse=True)
    return normalized


def _summarize_patient(
    patient_id: str, entries: List[Dict[str, Any]]
) -> Dict[str, Any]:
    normalized = _normalize_entries(entries)
    latest = normalized[0] if normalized else None
    return {
        "patient_id": patient_id,
        "entry_count": len(entries),
        "latest_verdict": latest["verdict"] if latest else "No data",
        "latest_timestamp": latest["timestamp"] if latest else "No data",
    }


def verify_credentials(credentials: HTTPBasicCredentials = Depends(SECURITY)) -> str:
    superusers = auth_store.read_credentials("superusers")
    if not superusers:
        raise HTTPException(
            status_code=status.HTTP_500_INTERNAL_SERVER_ERROR,
            detail="No superusers defined in auth.json",
        )

    stored_password = superusers.get(credentials.username)
    if not stored_password or not secrets.compare_digest(
        credentials.password, stored_password
    ):
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid superuser credentials",
            headers={"WWW-Authenticate": "Basic"},
        )
    return credentials.username


@app.get("/", response_class=HTMLResponse)
async def list_patients(
    request: Request, username: str = Depends(verify_credentials)
) -> HTMLResponse:
    diagnosis = diagnosis_store.read_entries()
    patient_rows = [
        _summarize_patient(patient_id, entries)
        for patient_id, entries in diagnosis.items()
    ]
    patient_rows.sort(key=lambda row: row["patient_id"])
    return TEMPLATES.TemplateResponse(
        request,
        "patients.html",
        {
            "patients": patient_rows,
            "username": username,
        },
    )


@app.get("/patients/{patient_id}", response_class=HTMLResponse)
async def patient_detail(
    patient_id: str, request: Request, username: str = Depends(verify_credentials)
) -> HTMLResponse:
    diagnosis = diagnosis_store.read_entries()
    entries = diagnosis.get(patient_id)
    if entries is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND, detail="Patient not found"
        )

    normalized_entries = _normalize_entries(entries)
    return TEMPLATES.TemplateResponse(
        request,
        "patient_detail.html",
        {
            "patient_id": patient_id,
            "entries": normalized_entries,
            "username": username,
        },
    )


@app.get("/events")
async def event_stream(
    request: Request, username: str = Depends(verify_credentials)
) -> StreamingResponse:
    del username  # credentials already verified
    queue = await broadcaster.register()

    async def event_generator() -> AsyncGenerator[str, None]:
        try:
            while True:
                try:
                    payload = await asyncio.wait_for(queue.get(), timeout=15)
                except asyncio.TimeoutError:
                    if await request.is_disconnected():
                        break
                    continue
                data = json.dumps(payload)
                yield f"data: {data}\n\n"
                if await request.is_disconnected():
                    break
        finally:
            await broadcaster.unregister(queue)

    return StreamingResponse(event_generator(), media_type="text/event-stream")


@app.post("/refresh")
async def refresh_data(username: str = Depends(verify_credentials)) -> RedirectResponse:
    diagnosis_store.reload()
    return RedirectResponse(url="/", status_code=status.HTTP_303_SEE_OTHER)


@app.post("/api/classify-image")
async def classify_image_endpoint(
    payload: ClassifyPayload, username: str = Depends(verify_credentials)
) -> Dict[str, Any]:
    del username
    if not payload.photo_path:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="photo_path is required",
        )

    target = MODEL_SERVICE_URL.rstrip("/") + "/predict"
    try:
        async with httpx.AsyncClient(timeout=30) as client:
            response = await client.post(target, json={"image_path": payload.photo_path})
    except httpx.RequestError as exc:
        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail=f"Model service unavailable: {exc}",
        ) from exc

    content_type = response.headers.get("content-type", "")
    if response.status_code >= 400:
        detail: Any
        if "application/json" in content_type:
            try:
                detail = response.json().get("detail")
            except ValueError:
                detail = response.text
        else:
            detail = response.text
        raise HTTPException(
            status_code=response.status_code,
            detail=detail or "Model service error",
        )

    try:
        return response.json()
    except ValueError as exc:
        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail="Model service returned invalid JSON",
        ) from exc


@app.post("/api/train-model")
async def train_model_endpoint(
    payload: TrainPayload | None = None,
    username: str = Depends(verify_credentials),
) -> Dict[str, Any]:
    del username
    request_payload = payload.model_dump(exclude_none=True) if payload else {}
    target = MODEL_SERVICE_URL.rstrip("/") + "/train"
    try:
        async with httpx.AsyncClient(timeout=600) as client:
            response = await client.post(target, json=request_payload or {})
    except httpx.RequestError as exc:
        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail=f"Model service unavailable: {exc}",
        ) from exc

    content_type = response.headers.get("content-type", "")
    body: Any
    if "application/json" in content_type:
        try:
            body = response.json()
        except ValueError:
            body = None
    else:
        body = None

    if response.status_code >= 400:
        detail = body.get("detail") if isinstance(body, dict) else response.text
        raise HTTPException(
            status_code=response.status_code,
            detail=detail or "Model service error",
        )

    if body is None:
        raise HTTPException(
            status_code=status.HTTP_502_BAD_GATEWAY,
            detail="Model service returned invalid JSON",
        )
    return body


@app.get("/logout", response_class=HTMLResponse)
async def logout() -> HTMLResponse:
    response = HTMLResponse("<p>Logged out.</p>")
    response.status_code = status.HTTP_401_UNAUTHORIZED
    response.headers["WWW-Authenticate"] = "Basic"
    return response


@app.get("/healthz")
async def healthcheck() -> Dict[str, str]:
    return {"status": "ok"}


if __name__ == "__main__":
    import uvicorn

    uvicorn.run(
        "pyservice.app.main:app",
        host="0.0.0.0",
        port=int(os.getenv("FASTAPI_PORT", "8000")),
        reload=True,
    )
