import json
import os
import secrets
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List, Optional

from fastapi import Depends, FastAPI, HTTPException, Request, status
from fastapi.responses import HTMLResponse, RedirectResponse
from fastapi.security import HTTPBasic, HTTPBasicCredentials
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates

SERVICE_ROOT = Path(__file__).resolve().parent.parent
DEFAULT_CONFIG_DIR = SERVICE_ROOT.parent / "src" / "configs"
DEFAULT_ASSETS_DIR = SERVICE_ROOT.parent / "src" / "assets"

CONFIG_DIR = Path(os.getenv("CONFIG_DIR", DEFAULT_CONFIG_DIR))
ASSETS_DIR = Path(os.getenv("ASSETS_DIR", DEFAULT_ASSETS_DIR))
FASTAPI_TITLE = os.getenv("FASTAPI_TITLE", "Diagnosis Dashboard")

app = FastAPI(title=FASTAPI_TITLE, version="0.1.0")
app.mount(
    "/assets", StaticFiles(directory=str(ASSETS_DIR), check_dir=False), name="assets"
)

TEMPLATES = Jinja2Templates(directory=str(SERVICE_ROOT / "templates"))
SECURITY = HTTPBasic()


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


@app.post("/refresh")
async def refresh_data(username: str = Depends(verify_credentials)) -> RedirectResponse:
    diagnosis_store.reload()
    return RedirectResponse(url="/", status_code=status.HTTP_303_SEE_OTHER)


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
