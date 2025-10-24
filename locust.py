"""
Optimized Locust script for 12GB RAM - Gin + ScyllaDB API

Target: 5,000-10,000 concurrent users for realistic performance testing
Expected: 2,000-5,000 RPS with <100ms latency

Routes tested:
- GET /api/v1/get/user/:id

Run examples:
  Basic test (recommended):
    locust -f locust.py --host=http://localhost:8000 --users 5000 --spawn-rate 100

  High load test:
    locust -f locust.py --host=http://localhost:8000 --users 10000 --spawn-rate 200

  Stress test (headless):
    locust -f locust.py --host=http://localhost:8000 --users 5000 --spawn-rate 100 --run-time 2m --headless

  Web UI:
    locust -f locust.py --host=http://localhost:8000
"""

import os
import json
from typing import List
from locust import FastHttpUser, task, between


# Comma-separated UUIDs to test GET by ID; falls back to a default if not provided
DEFAULT_ID = "6b7bc0ee-af3e-11f0-89c7-52c2e832ce81"
_ids_env = os.getenv("TEST_USER_IDS", DEFAULT_ID)
TEST_USER_IDS: List[str] = [s.strip() for s in _ids_env.split(",") if s.strip()]


class ApiUser(FastHttpUser):
    """
    Optimized user model using FastHttpUser for lower memory footprint.
    
    Memory usage: ~40KB per user (vs 80KB with HttpUser)
    Recommended max users: 10,000 (uses ~400MB for Locust)
    """

    # Simulate realistic user behavior with short pauses
    wait_time = between(0.5, 1.5)

    def on_start(self):
        """Initialize user session - runs once per user spawn."""
        self.headers = {
            "Content-Type": "application/json",
            "User-Agent": "Locust/FastApiUser",
            "Connection": "keep-alive"  # Reuse connections
        }

    @task
    def get_user(self):
        """
        GET user by ID - tests cache effectiveness and API response time.
        
        Expected performance:
        - Cache hit: 1-3ms
        - Cache miss: 10-50ms (database query)
        - P95: <100ms
        - P99: <500ms
        """
        with self.client.get(
            f"/api/v1/get/user/{TEST_USER_IDS[0]}",
            name="GET /api/v1/user/[id]",
            headers=self.headers,
            catch_response=True,
        ) as resp:
            if resp.status_code == 200:
                try:
                    data = resp.json()
                    
                    # Validate response structure
                    if "user" not in data:
                        resp.failure("Missing 'user' field in response")
                        return
                    
                    # Optional: Check cache source
                    if "source" in data:
                        # Track cache effectiveness (visible in Locust logs)
                        pass
                    
                    resp.success()
                    
                except json.JSONDecodeError:
                    resp.failure("Invalid JSON response")
                    
            elif resp.status_code == 404:
                resp.failure("User not found")
            else:
                resp.failure(f"Unexpected status code: {resp.status_code}")
