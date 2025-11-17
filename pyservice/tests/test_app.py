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
