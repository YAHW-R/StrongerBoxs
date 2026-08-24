from datetime import datetime, timedelta, timezone

from tests.conftest import (
    auth_headers,
    register_and_login,
    sample_item_uuid,
    sample_note_payload,
)


def push_note(client, token, uuid_, version=1, payload=None, deleted=False):
    return client.post(
        "/items/push",
        headers=auth_headers(token),
        json={
            "items": [
                {
                    "item_uuid": uuid_,
                    "kind": "note",
                    "payload": payload or sample_note_payload(),
                    "version": version,
                    "deleted": deleted,
                    "updated_at": datetime.now(timezone.utc).isoformat(),
                }
            ]
        },
    )


def test_push_pull_roundtrip(client):
    token = register_and_login(client, "sync1")
    uid = sample_item_uuid()

    r = push_note(client, token, uid, version=2)
    assert r.status_code == 200
    body = r.json()
    assert [a["item_uuid"] for a in body["accepted"]] == [uid]
    assert body["skipped"] == []

    r = client.post("/items/pull", headers=auth_headers(token), json={"since": None})
    assert r.status_code == 200
    data = r.json()
    assert len(data["items"]) == 1
    item = data["items"][0]
    assert item["item_uuid"] == uid
    assert item["payload"]["title"] == "sb1.AAAAtitle"  # opaco: sale igual que entró
    assert item["version"] == 2
    # JSON devuelve ISO strings; SQLite puede devolver datetimes naive
    # (PostgreSQL conserva tz): normalizamos a UTC antes de comparar.
    def aware(iso: str) -> datetime:
        dt = datetime.fromisoformat(iso)
        return dt.replace(tzinfo=timezone.utc) if dt.tzinfo is None else dt

    assert aware(data["server_time"]) >= aware(item["synced_at"]) - timedelta(seconds=1)


def test_push_lww_older_date_skipped(client):
    """LWW POR FECHA: gana el updated_at más nuevo, no la versión."""
    token = register_and_login(client, "sync2")
    uid = sample_item_uuid()
    late = datetime.now(timezone.utc)
    early = late - timedelta(hours=1)

    r = client.post("/items/push", headers=auth_headers(token), json={
        "items": [{"item_uuid": uid, "kind": "note", "payload": sample_note_payload(),
                   "version": 1, "updated_at": late.isoformat()}]})

    # Versión MÁS ALTA pero fecha más vieja ⇒ se salta (la fecha manda).
    r = client.post("/items/push", headers=auth_headers(token), json={
        "items": [{"item_uuid": uid, "kind": "note",
                   "payload": sample_note_payload("sb1.OLDold"),
                   "version": 3, "updated_at": early.isoformat()}]})
    skipped = r.json()["skipped"][0]
    assert skipped["reason"] == "stale_date"
    assert skipped["server_version"] == 1

    items = client.post("/items/pull", headers=auth_headers(token), json={}).json()["items"]
    assert items[0]["payload"]["title"] != "sb1.OLDold"


def test_push_equal_date_skipped(client):
    token = register_and_login(client, "sync3")
    uid = sample_item_uuid()
    ts = datetime.now(timezone.utc).isoformat()
    for _ in range(2):
        r = client.post("/items/push", headers=auth_headers(token), json={
            "items": [{"item_uuid": uid, "kind": "note", "payload": sample_note_payload(),
                       "version": 1, "updated_at": ts}]})
    assert r.json()["skipped"][0]["reason"] == "stale_date"


def test_delete_flag_propagates_via_tombstone(client):
    token = register_and_login(client, "sync4")
    uid = sample_item_uuid()
    push_note(client, token, uid, version=1)
    push_note(client, token, uid, version=2, deleted=True)

    items = client.post("/items/pull", headers=auth_headers(token), json={}).json()["items"]
    assert items[0]["deleted"] is True


def test_pull_delta_since(client):
    token = register_and_login(client, "sync5")
    u1, u2 = sample_item_uuid(), sample_item_uuid()

    push_note(client, token, u1)
    first = client.post("/items/pull", headers=auth_headers(token), json={}).json()
    since = first["server_time"]

    push_note(client, token, u2, version=3)
    delta = client.post(
        "/items/pull", headers=auth_headers(token), json={"since": since}
    ).json()
    ids = {i["item_uuid"] for i in delta["items"]}
    assert ids == {u2}


def test_zk_rejects_plaintext_sensitive_field(client):
    """Un campo sensible en claro debe ser rechazado: el servidor jamás
    acepta contenido legible (Zero-Knowledge por contrato)."""
    token = register_and_login(client, "zk1")
    uid = sample_item_uuid()

    bad_payload = {"title": "Texto en claro prohibido", "body": ""}
    r = push_note(client, token, uid, payload=bad_payload)
    assert r.status_code == 422
    detail = str(r.json())
    assert "sb1." in detail or "cifrado" in detail


def test_zk_rejects_unknown_metadata_keys(client):
    token = register_and_login(client, "zk2")
    r = push_note(
        client,
        token,
        sample_item_uuid(),
        payload={"title": "sb1.X", "body": "", "sneaky": "valor"},
    )
    assert r.status_code == 422


def test_requires_bearer_token(client):
    r = client.post("/items/push", json={"items": []})
    assert r.status_code in (401, 403)


def test_items_isolated_per_user(client):
    t1 = register_and_login(client, "user-a")
    t2 = register_and_login(client, "user-b")

    push_note(client, t1, sample_item_uuid())

    mine = client.post("/items/pull", headers=auth_headers(t2), json={}).json()["items"]
    assert mine == []


def test_secret_template_and_extra_accepted(client):
    """Las plantillas del cliente viajan como metadato + blob cifrado."""
    token = register_and_login(client, "tpl-user")
    uid = sample_item_uuid()

    payload = {
        "title": "sb1.BANCObanco",
        "username": "",
        "password": "sb1.CLAVEclave",
        "url": "",
        "notes": "",
        "template": "banco",
        "extra": "sb1.EXTRAextra",
    }
    r = client.post("/items/push", headers=auth_headers(token), json={
        "items": [{"item_uuid": uid, "kind": "secret", "payload": payload,
                   "version": 1, "updated_at": datetime.now(timezone.utc).isoformat()}]})
    assert r.status_code == 200, r.text
    assert r.json()["accepted"][0]["status"] == "accepted"

    items = client.post("/items/pull", headers=auth_headers(token), json={}).json()["items"]
    assert items[0]["payload"]["template"] == "banco"
    assert items[0]["payload"]["extra"].startswith("sb1.")


def test_secret_extra_plaintext_rejected(client):
    token = register_and_login(client, "tpl-bad")
    r = client.post("/items/push", headers=auth_headers(token), json={
        "items": [{"item_uuid": sample_item_uuid(), "kind": "secret",
                   "payload": {"title": "", "extra": "json-en-claro"},
                   "version": 1,
                   "updated_at": datetime.now(timezone.utc).isoformat()}]})
    assert r.status_code == 422


def test_secret_template_invalid_slug_rejected(client):
    token = register_and_login(client, "tpl-slug")
    r = client.post("/items/push", headers=auth_headers(token), json={
        "items": [{"item_uuid": sample_item_uuid(), "kind": "secret",
                   "payload": {"title": "", "template": "NO VALIDO"},
                   "version": 1,
                   "updated_at": datetime.now(timezone.utc).isoformat()}]})
    assert r.status_code == 422
