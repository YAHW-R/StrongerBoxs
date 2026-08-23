from tests.conftest import (
    TEST_PASSWORD,
    TEST_SALT,
    TEST_VERIFIER,
    register_and_login,
)


def test_health(client):
    r = client.get("/health")
    assert r.status_code == 200
    assert r.json() == {"status": "ok"}


def test_salt_known_vs_unknown_same_shape(client):
    register_and_login(client, "bob")
    known = client.post("/auth/salt", json={"username": "bob"}).json()["salt"]
    unknown = client.post("/auth/salt", json={"username": "nobody"}).json()["salt"]
    assert isinstance(known, str) and len(known) >= 16
    assert isinstance(unknown, str) and len(unknown) >= 16
    # determinista para el mismo usuario desconocido
    again = client.post("/auth/salt", json={"username": "nobody"}).json()["salt"]
    assert unknown == again


def test_register_login_roundtrip(client):
    token = register_and_login(client, "carol")
    assert len(token.split(".")) == 3  # JWT con header.payload.signature

    r = client.post("/auth/login", json={"username": "carol", "verifier": TEST_VERIFIER})
    assert r.status_code == 200
    body = r.json()
    assert body["token_type"] == "bearer"
    assert body["expires_in"] > 0
    assert len(body["user_id"]) == 36


def test_login_wrong_verifier_401(client):
    register_and_login(client, "dave")
    bad = "b" * 64
    r = client.post("/auth/login", json={"username": "dave", "verifier": bad})
    assert r.status_code == 401


def test_login_unknown_user_401_generic(client):
    r = client.post("/auth/login", json={"username": "ghost", "verifier": TEST_VERIFIER})
    assert r.status_code == 401
    assert r.json()["detail"] == "Credenciales inválidas"  # sin pistas de enumeración


def test_register_duplicate_409(client):
    register_and_login(client, "erin")
    r = client.post(
        "/auth/register",
        json={"username": "erin", "salt": TEST_SALT, "verifier": TEST_VERIFIER},
    )
    assert r.status_code == 409


def test_register_rejects_plaintextish_username(client):
    r = client.post(
        "/auth/register",
        json={"username": "Nombre Con Espacios", "salt": TEST_SALT, "verifier": TEST_VERIFIER},
    )
    assert r.status_code == 422


def test_me_protected_without_token(client):
    r = client.post(
        "/items/pull",
        json={},
        headers={"Authorization": ""},
    )
    assert r.status_code in (401, 403)
