"""Fixtures: motor SQLite por test + override de la dependencia get_db.

No usamos context-manager en TestClient para que NO corra el lifespan
(create_all contra PostgreSQL); las tablas se crean aquí sobre SQLite.
"""

import uuid

import pytest
from fastapi.testclient import TestClient
from sqlalchemy import create_engine
from sqlalchemy.orm import sessionmaker

from app.db import Base, get_db
from app.main import app

TEST_PASSWORD = "maestra-correcta-1"
# verifier simulado: hex(SHA256) de 64 caracteres (el cliente lo deriva).
TEST_VERIFIER = "a" * 64
TEST_SALT = "c2FsdC1zYWx0LXNhbHQ="  # base64 cualquiera >=16 chars


@pytest.fixture()
def client(tmp_path):
    engine = create_engine(
        f"sqlite:///{tmp_path}/test.db", connect_args={"check_same_thread": False}
    )
    TestingSession = sessionmaker(bind=engine, autoflush=False, expire_on_commit=False)
    Base.metadata.create_all(engine)

    def _override_get_db():
        db = TestingSession()
        try:
            yield db
        finally:
            db.close()

    app.dependency_overrides[get_db] = _override_get_db
    yield TestClient(app)
    app.dependency_overrides.clear()


def register_and_login(client: TestClient, username: str = "alice") -> str:
    """Registra un usuario y devuelve el token de acceso."""
    r = client.post(
        "/auth/register",
        json={"username": username, "salt": TEST_SALT, "verifier": TEST_VERIFIER},
    )
    assert r.status_code == 201, r.text
    return r.json()["access_token"]


def auth_headers(token: str) -> dict:
    return {"Authorization": f"Bearer {token}"}


def sample_note_payload(title_env: str = "sb1.AAAAtitle") -> dict:
    return {
        "title": title_env,
        "body": "sb1.BBBBbody",
        "color": "#F9AB00",
        "pinned": False,
    }


def sample_item_uuid() -> str:
    return str(uuid.uuid4())
