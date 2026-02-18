"""initial schema

Revision ID: 20260218_0001
Revises:
Create Date: 2026-02-18 00:00:00
"""

from alembic import op
from app.db import models  # noqa: F401
from app.db.base import Base

revision = "20260218_0001"
down_revision = None
branch_labels = None
depends_on = None


def upgrade() -> None:
    bind = op.get_bind()
    Base.metadata.create_all(bind)


def downgrade() -> None:
    bind = op.get_bind()
    Base.metadata.drop_all(bind)
