import importlib
import json
import os
from pathlib import Path

import pytest
from fastapi.testclient import TestClient


@pytest.fixture()
def client(tmp_path, monkeypatch):
    config_dir = tmp_path / "configs"
    assets_dir = tmp_path / "assets"
    config_dir.mkdir()
    assets_dir.mkdir()

    auth_payload = {
        "users": [{"username": "usr", "password": "pwd"}],
        "superusers": [{"username": "doctor", "password": "doctor"}],
    }
    diagnosis_payload = {
        "usr": [
            {
                "photo_path": "assets/example.jpg",
                "timestamp": "2024-01-01T12:30:00Z",
                "verdict": True,
                "rationale": "Lesion detected",
            }
        ]
    }

    (config_dir / "auth.json").write_text(json.dumps(auth_payload), encoding="utf-8")
    (config_dir / "diagnosis.json").write_text(
        json.dumps(diagnosis_payload), encoding="utf-8"
    )
    (assets_dir / "example.jpg").write_bytes(b"fake-bytes")

    monkeypatch.setenv("CONFIG_DIR", str(config_dir))
    monkeypatch.setenv("ASSETS_DIR", str(assets_dir))
    monkeypatch.setenv("MODEL_SERVICE_URL", "http://modelservice.test")

    module = importlib.import_module("pyservice.app.main")
    module = importlib.reload(module)

    with TestClient(module.app) as test_client:
        test_client.config_dir = config_dir  # type: ignore[attr-defined]
        yield test_client


def test_overview_requires_auth(client: TestClient):
    response = client.get("/")
    assert response.status_code == 401


def test_overview_lists_patients(client: TestClient):
    response = client.get("/", auth=("doctor", "doctor"))
    assert response.status_code == 200
    assert "usr" in response.text
    assert "Lesion detected" not in response.text  # overview hides rationale


def test_patient_detail_view(client: TestClient):
    response = client.get("/patients/usr", auth=("doctor", "doctor"))
    assert response.status_code == 200
    assert "Lesion detected" in response.text
    assert "View photo" in response.text


def test_refresh_endpoint_reloads_from_disk(client: TestClient):
    response = client.get("/", auth=("doctor", "doctor"))
    assert "usr" in response.text
    config_dir = getattr(client, "config_dir")
    diagnosis_path = Path(config_dir) / "diagnosis.json"
    payload = {
        "other": [
            {
                "photo_path": "assets/example.jpg",
                "timestamp": "2024-02-02T10:00:00Z",
                "verdict": False,
                "rationale": "Follow-up",
            }
        ]
    }
    diagnosis_path.write_text(json.dumps(payload), encoding="utf-8")

    response_stale = client.get("/", auth=("doctor", "doctor"))
    assert "other" not in response_stale.text

    refresh_response = client.post(
        "/refresh", auth=("doctor", "doctor"), follow_redirects=False
    )
    assert refresh_response.status_code == 303

    response_fresh = client.get("/", auth=("doctor", "doctor"))
    assert "other" in response_fresh.text


def test_logout_forces_basic_auth_prompt(client: TestClient):
    response = client.get("/logout")
    assert response.status_code == 401
    assert response.headers.get("www-authenticate") == "Basic"


def test_regular_user_cannot_access_dashboard(client: TestClient):
    response = client.get("/", auth=("usr", "pwd"))
    assert response.status_code == 401


def test_classify_image_endpoint_calls_model_service(client: TestClient, monkeypatch):
    captured = {}

    class DummyResponse:
        def __init__(self) -> None:
            self.status_code = 200
            self.headers = {"content-type": "application/json"}

        def json(self):
            return {"label": "c", "probability": 0.91}

    class DummyClient:
        async def __aenter__(self):
            return self

        async def __aexit__(self, exc_type, exc, tb):
            return False

        async def post(self, url, json):
            captured["url"] = url
            captured["json"] = json
            return DummyResponse()

    monkeypatch.setattr("pyservice.app.main.httpx.AsyncClient", lambda **_: DummyClient())

    response = client.post(
        "/api/classify-image",
        auth=("doctor", "doctor"),
        json={"photo_path": "assets/example.jpg"},
    )

    assert response.status_code == 200
    assert response.json()["label"] == "c"
    assert captured["url"].endswith("/predict")
    assert captured["json"] == {"image_path": "assets/example.jpg"}


def test_train_model_endpoint_calls_model_service(client: TestClient, monkeypatch):
    captured = {}

    class DummyResponse:
        def __init__(self) -> None:
            self.status_code = 200
            self.headers = {"content-type": "application/json"}

        def json(self):
            return {
                "baseline": {"val_accuracy": 0.8, "val_loss": 0.5, "epochs_trained": 5},
                "transfer": {"val_accuracy": 0.9, "val_loss": 0.3, "epochs_trained": 4},
            }

    class DummyClient:
        async def __aenter__(self):
            return self

        async def __aexit__(self, exc_type, exc, tb):
            return False

        async def post(self, url, json):
            captured["url"] = url
            captured["json"] = json
            return DummyResponse()

    monkeypatch.setattr("pyservice.app.main.httpx.AsyncClient", lambda **_: DummyClient())

    response = client.post(
        "/api/train-model",
        auth=("doctor", "doctor"),
        json={"epochs": 7, "augment": False},
    )

    assert response.status_code == 200
    assert response.json()["transfer"]["val_accuracy"] == 0.9
    assert captured["url"].endswith("/train")
    assert captured["json"] == {"epochs": 7, "augment": False}
