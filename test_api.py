import os
import json
import sys
import urllib.request
import urllib.error

env_vars = {}
for fname in (".env", ".env.production"):
    if os.path.exists(fname):
        with open(fname, "r") as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    key, val = line.split("=", 1)
                    env_vars.setdefault(key.strip(), val.strip())

api_url = (os.getenv("API_URL") or env_vars.get("API_URL") or "http://localhost:8000").rstrip("/")
test_email = os.getenv("TEST_EMAIL") or env_vars.get("DEV_SEED_EMAIL") or "broker@dispatch.owera.ca"
test_password = os.getenv("TEST_PASSWORD") or env_vars.get("DEV_SEED_PASSWORD") or ""

print(f"Running Integration Tests against API: {api_url}\n")

def make_request(url: str, method: str = "GET", headers: dict = None, body: dict = None):
    req_headers = {"Content-Type": "application/json"}
    if headers:
        req_headers.update(headers)
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(url, data=data, headers=req_headers, method=method)
    try:
        with urllib.request.urlopen(req) as response:
            response_body = response.read().decode("utf-8")
            return response.getcode(), json.loads(response_body) if response_body else {}
    except urllib.error.HTTPError as e:
        response_body = e.read().decode("utf-8")
        try:
            err_data = json.loads(response_body)
        except Exception:
            err_data = response_body
        return e.code, err_data
    except urllib.error.URLError as e:
        print(f"Connection Error: {e.reason}")
        return 0, {}

failed_tests = 0

def test_root():
    global failed_tests
    print("Test 1: GET / (Root Healthcheck)")
    code, data = make_request(f"{api_url}/")
    if code == 200 and data.get("status") in ("online", "running"):
        print("  PASS: Root status is running.")
    else:
        print(f"  FAIL: Expected 200/running, got Status={code}, Response={data}")
        failed_tests += 1

def test_spoofed_header_rejected():
    global failed_tests
    print("\nTest 2: Verify X-User-Email spoofing is rejected (401)")
    code, _ = make_request(f"{api_url}/api/quotes", headers={"X-User-Email": test_email})
    if code == 401:
        print("  PASS: Unsigned X-User-Email header rejected with 401.")
    else:
        print(f"  FAIL: Expected 401 for unsigned X-User-Email header, got {code}")
        failed_tests += 1

def test_authenticated_flow():
    global failed_tests
    if not test_password:
        print("\nSkipping authenticated JWT CRUD tests (set TEST_PASSWORD or DEV_SEED_PASSWORD to run).")
        return

    print("\nTest 3: POST /api/auth/login (JWT Authentication)")
    code, auth_data = make_request(
        f"{api_url}/api/auth/login",
        method="POST",
        body={"email": test_email, "password": test_password},
    )
    if code != 200 or not auth_data.get("access_token"):
        print(f"  FAIL: Login failed Status={code}, Response={auth_data}")
        failed_tests += 1
        return

    headers = {"Authorization": f"Bearer {auth_data['access_token']}"}
    print("  PASS: Signed JWT access token issued.")

    print("\nTest 4: GET /api/billing/summary (Option A Entitlements)")
    code, bill_data = make_request(f"{api_url}/api/billing/summary", headers=headers)
    if code == 200 and "plan_tier" in bill_data:
        print(f"  PASS: Billing summary returned tier={bill_data['plan_tier']}.")
    else:
        print(f"  FAIL: Billing summary failed Status={code}, Response={bill_data}")
        failed_tests += 1

    print("\nTest 5: POST /api/quotes/{id}/upload-url path traversal rejected (400)")
    code, _ = make_request(
        f"{api_url}/api/quotes/Q-1001/upload-url?filename=../../evil.pdf",
        method="POST",
        headers=headers,
    )
    if code == 400:
        print("  PASS: Path traversal upload filename rejected with 400.")
    else:
        print(f"  FAIL: Expected 400 on path traversal filename, got {code}")
        failed_tests += 1

test_root()
test_spoofed_header_rejected()
test_authenticated_flow()

print("\n--- Test Run Summary ---")
if failed_tests == 0:
    print("ALL TESTS PASSED SUCCESSFULLY!")
    sys.exit(0)
else:
    print(f"{failed_tests} TESTS FAILED.")
    sys.exit(1)
