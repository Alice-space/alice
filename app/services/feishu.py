from __future__ import annotations

import base64
import hashlib
import hmac
import json
import logging
from typing import Any

import httpx
from cryptography.hazmat.primitives import padding
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes

from app.config import Settings

logger = logging.getLogger(__name__)


class FeishuService:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings

    def verify_signature(self, headers: dict[str, str], body: bytes) -> bool:
        secret = self.settings.feishu_signing_secret
        if not secret:
            return True

        signature = (
            headers.get("x-lark-signature")
            or headers.get("x-lark-request-signature")
            or headers.get("X-Lark-Signature")
            or ""
        )
        timestamp = (
            headers.get("x-lark-request-timestamp") or headers.get("X-Lark-Request-Timestamp") or ""
        )
        nonce = headers.get("x-lark-request-nonce") or headers.get("X-Lark-Request-Nonce") or ""
        if not signature:
            return False

        mac = hmac.new(
            secret.encode("utf-8"), f"{timestamp}{nonce}".encode("utf-8") + body, hashlib.sha256
        )
        expected_hex = mac.hexdigest()
        expected_b64 = base64.b64encode(mac.digest()).decode("utf-8")
        return signature in {expected_hex, expected_b64}

    def decrypt_if_needed(self, payload: dict[str, Any]) -> dict[str, Any]:
        encrypted = payload.get("encrypt")
        if not encrypted:
            return payload

        key_raw = self.settings.feishu_encrypt_key
        if not key_raw:
            raise ValueError("encrypted payload received but ALICE_FEISHU_ENCRYPT_KEY is not set")

        key = key_raw.encode("utf-8")
        if len(key) < 32:
            key = key.ljust(32, b"0")
        if len(key) > 32:
            key = key[:32]

        encrypted_bytes = base64.b64decode(encrypted)
        cipher = Cipher(algorithms.AES(key), modes.CBC(key[:16]))
        decryptor = cipher.decryptor()
        padded = decryptor.update(encrypted_bytes) + decryptor.finalize()

        unpadder = padding.PKCS7(algorithms.AES.block_size).unpadder()
        plain = unpadder.update(padded) + unpadder.finalize()
        return json.loads(plain.decode("utf-8"))

    async def send_message(
        self, receive_id: str, text: str, receive_id_type: str = "chat_id"
    ) -> dict[str, Any]:
        token = self.settings.feishu_bot_token
        if not token:
            logger.info("feishu token missing; skip outbound message")
            return {"ok": False, "error": "feishu bot token missing"}

        url = f"{self.settings.feishu_base_url}/open-apis/im/v1/messages"
        headers = {
            "Authorization": f"Bearer {token}",
            "Content-Type": "application/json",
        }
        params = {"receive_id_type": receive_id_type}
        body = {
            "receive_id": receive_id,
            "msg_type": "text",
            "content": json.dumps({"text": text}),
        }
        async with httpx.AsyncClient(timeout=20) as client:
            response = await client.post(url, headers=headers, params=params, json=body)
        if response.status_code >= 400:
            return {"ok": False, "error": f"{response.status_code} {response.text[:200]}"}
        return {"ok": True, "data": response.json()}
