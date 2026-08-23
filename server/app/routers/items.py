"""Sincronización de ítems cifrados.

El servidor solo guarda JSONB opaco y decide conflictos POR FECHA:
gana el `updated_at` estrictamente mayor (LWW temporal). El campo
`version` es informativo (contador monótono del cliente).
"""

import json
from datetime import datetime, timezone

from fastapi import APIRouter, Depends
from sqlalchemy import select
from sqlalchemy.orm import Session

from ..config import get_settings
from ..db import get_db
from ..models import User, VaultItem
from ..schemas import (
    ItemIn,
    ItemOut,
    ItemResult,
    PullRequest,
    PullResponse,
    PushRequest,
    PushResponse,
)
from ..security import get_current_user

router = APIRouter(prefix="/items", tags=["items"])
settings = get_settings()


def _payload_size(item: ItemIn) -> int:
    return len(json.dumps(item.payload).encode())


def _aware(dt: datetime) -> datetime:
    """SQLite puede devolver datetimes sin tz (PostgreSQL no): normalizar."""
    if dt.tzinfo is None:
        return dt.replace(tzinfo=timezone.utc)
    return dt


@router.post("/push", response_model=PushResponse)
def push(body: PushRequest, user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    accepted: list[ItemResult] = []
    skipped: list[ItemResult] = []

    for item in body.items:
        if _payload_size(item) > settings.max_payload_bytes:
            skipped.append(ItemResult(item_uuid=item.item_uuid, status="skipped",
                                      reason="payload_too_large"))
            continue

        row = db.execute(
            select(VaultItem).where(
                VaultItem.user_id == user.id,
                VaultItem.item_uuid == item.item_uuid,
            )
        ).scalar_one_or_none()

        if row is None:
            db.add(
                VaultItem(
                    user_id=user.id,
                    item_uuid=item.item_uuid,
                    kind=item.kind,
                    payload=item.payload,
                    version=item.version,
                    deleted=item.deleted,
                    client_updated_at=item.updated_at,
                )
            )
            accepted.append(ItemResult(item_uuid=item.item_uuid, status="accepted"))

        elif item.updated_at > _aware(row.client_updated_at):
            # LWW POR FECHA (requisito de producto): gana la actualización
            # con `updated_at` más nueva, sea del cliente que sea.
            row.payload = item.payload
            row.kind = item.kind
            row.version = item.version
            row.deleted = item.deleted
            row.client_updated_at = item.updated_at
            row.synced_at = datetime.now(timezone.utc)
            accepted.append(ItemResult(item_uuid=item.item_uuid, status="accepted"))

        else:
            skipped.append(
                ItemResult(
                    item_uuid=item.item_uuid,
                    status="skipped",
                    reason="stale_date",
                    server_version=row.version,
                )
            )

    db.commit()
    return PushResponse(accepted=accepted, skipped=skipped)


@router.post("/pull", response_model=PullResponse)
def pull(
    body: PullRequest, user: User = Depends(get_current_user), db: Session = Depends(get_db)
):
    query = select(VaultItem).where(VaultItem.user_id == user.id)
    if body.since is not None:
        query = query.where(VaultItem.synced_at > body.since)
    query = query.order_by(VaultItem.synced_at.asc()).limit(settings.max_items_per_push)

    rows = db.execute(query).scalars().all()
    items = [
        ItemOut(
            item_uuid=r.item_uuid,
            kind=r.kind,
            payload=r.payload,
            version=r.version,
            deleted=r.deleted,
            updated_at=r.client_updated_at,
            synced_at=r.synced_at,
        )
        for r in rows
    ]
    return PullResponse(items=items, server_time=datetime.now(timezone.utc))
