"""
Production-grade Locust script for the Gin + ScyllaDB API

Routes discovered:
- GET    /api/v1/health
- POST   /api/v1/create/user
- GET    /api/v1/get/user/:id
- GET    /api/v1/cache/metrics

Run examples:
- locust -f locust.py --host=http://localhost:3000
- locust -f locust.py --list
"""

import os
import json
from typing import List
from locust import HttpUser, task, between


# Comma-separated UUIDs to test GET by ID; falls back to a default if not provided
DEFAULT_ID = "6b7bc0ee-af3e-11f0-89c7-52c2e832ce81"
_ids_env = os.getenv("TEST_USER_IDS", DEFAULT_ID)
TEST_USER_IDS: List[str] = [s.strip() for s in _ids_env.split(",") if s.strip()]


class ApiUser(HttpUser):
    """User model for Locust. Locust requires a subclass of HttpUser or FastHttpUser."""

    wait_time = between(1, 2)

    def on_start(self):
        # Common headers; add auth headers here if your API requires it
        self.headers = {
            "Content-Type": "application/json",
            "User-Agent": "Locust/ApiUser"
        }

    @task
    def get_user(self):
        """Get user details endpoint."""
        with self.client.get(
            "/api/v1/get/user/{}".format(TEST_USER_IDS[0]),
            name="GET /api/v1/user/[id]",
            headers=self.headers,
            catch_response=True,
        ) as resp:
            if resp.status_code == 200:
                # Optionally validate structure
                try:
                    _ = resp.json()
                except json.JSONDecodeError:
                    resp.failure("Invalid JSON body")
                    return
                resp.success()
            else:
                resp.failure(f"Unexpected status: {resp.status_code}")
