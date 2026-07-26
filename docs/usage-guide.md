# BastionGate Demo Usage Guide

This guide covers local setup, the EICAR blocked-file demonstration, the clean-file release path, and common operational checks for Acme People.

## Architecture

The browser talks only to the Acme People server. The Go facade adds the BastionGate API-client credential and streams the upload to the locally running BastionGate instance.

```text
Browser
  |
  | same-origin HTTP
  v
Acme People Go facade
  |
  | Bearer API-client key
  v
BastionGate -> quarantine -> scan and policy -> trust decision
```

The API-client key never enters browser JavaScript, browser storage, or browser network requests. Acme People does not retain uploaded files. A file can be downloaded and used as the profile photo only after BastionGate returns `RELEASED`.

## Prerequisites

- Docker Engine with Docker Compose v2.
- BastionGate running locally, including its scan worker, ClamAV, PostgreSQL, and quarantine storage.
- A BastionGate API-client key authorized for the configured policy.
- BastionGate reachable from the host at <http://localhost:8080>, unless you intentionally use different URLs.

Confirm BastionGate is healthy:

```bash
curl --fail http://localhost:8080/actuator/health
```

## Configure the demo

Create the local environment file:

```bash
cp .env.example .env
```

Set at least the API-client key:

```dotenv
BASTIONGATE_API_KEY=replace-with-local-api-client-key
BASTIONGATE_INTERNAL_URL=http://host.docker.internal:8080
BASTIONGATE_PUBLIC_URL=http://localhost:8080
BASTIONGATE_POLICY_CODE=DEFAULT
ACME_PEOPLE_PORT=3000
```

`BASTIONGATE_INTERNAL_URL` is resolved by the Acme People container. `BASTIONGATE_PUBLIC_URL` is opened by the user's browser for the BastionGate audit page.

If both applications run on the same Docker network, use the BastionGate service name for `BASTIONGATE_INTERNAL_URL`. Keep `BASTIONGATE_PUBLIC_URL` browser-reachable.

## Start with Docker

Build and start the application:

```bash
docker compose up --build
```

Open <http://localhost:3000>. The connection badge should change to **BastionGate connected**. Upload remains disabled while BastionGate is unavailable or the browser-safe runtime configuration cannot be loaded.

To stop the application:

```bash
docker compose down
```

## Run the EICAR blocked-file journey

1. Open **Acme People** at <http://localhost:3000>.
2. Confirm the connection badge is green.
3. Select **Use the EICAR demo file**.
4. Confirm the selected filename is `avatar.php.jpg`, its size is 68 bytes, and its trust label is `UNTRUSTED`.
5. Select **Send through BastionGate**.
6. Observe the `202 Accepted` evidence and lifecycle status.
7. Wait for the final blocked decision and ClamAV signature evidence.
8. Select **Continue to audit**.
9. Open the BastionGate file audit or view the redacted technical report.

The demo creates the standard EICAR test bytes in browser memory at runtime. EICAR is harmless and non-executable, but antivirus or endpoint-security software may intentionally alert when it is created or transmitted.

## Run the clean-file journey

1. Select a valid JPG or PNG under the displayed upload limit.
2. Send it through BastionGate.
3. Confirm the current avatar remains unchanged while the file is untrusted.
4. Wait for BastionGate to return `RELEASED`.
5. Confirm Acme People retrieves the file through its controlled download endpoint and then updates the avatar.

Files with blocked, failed, or review-required decisions never become the profile photo.

## Expected lifecycle

| State or verdict | Demo behavior |
|---|---|
| `RECEIVED`, `QUARANTINED`, `QUEUED`, `SCANNING` | Continue polling; the current avatar remains unchanged |
| `RELEASED` | Allow controlled download and apply the returned image |
| `BLOCKED` or `MALICIOUS` | Show the block decision and scan evidence |
| `REVIEW_REQUIRED` | Keep the file untrusted and require operator review |
| `FAILED`, `FAILED_SCAN`, or `FAILED_POLICY` | Keep the file untrusted and show a conservative failure decision |

Polling has a strict 60-second deadline. It honors `Retry-After` for rate limiting, retries temporary network and server failures, and stops immediately for permanent authentication, authorization, not-found, or malformed-response failures.

## API flow

The browser-facing facade maps requests to the BastionGate app-client contract:

| Browser request | BastionGate request | Purpose |
|---|---|---|
| `GET /api/health` | `GET /actuator/health` | Connection readiness |
| `GET /api/config` | Local safe configuration | Policy and upload-limit display |
| `POST /api/uploads` | `POST /api/v1/files/upload` | Stream an untrusted multipart upload |
| `GET /api/files/{fileId}/status` | `GET /api/v1/files/{fileId}/status` | Poll the trust decision |
| `GET /api/files/{fileId}/report` | `GET /api/v1/files/{fileId}/report` | Retrieve allow-listed audit evidence |
| `GET /api/files/{fileId}/download` | Status check, then controlled download | Release gate for clean files |

The facade supplies `Authorization`, `policyCode`, `sourceReference`, and `X-Correlation-ID` server-side.

## Run without Docker

Go 1.26 or newer is required:

```bash
export BASTIONGATE_API_KEY="replace-with-local-api-client-key"
export BASTIONGATE_INTERNAL_URL="http://localhost:8080"
export BASTIONGATE_PUBLIC_URL="http://localhost:8080"
export BASTIONGATE_POLICY_CODE="DEFAULT"
go run ./cmd/acme-people
```

Open <http://localhost:3000>.

## Verification

Run unit, race, static-analysis, and browser tests:

```bash
go test -race ./...
go vet ./...
npm install
npx playwright install chromium
npm test
npm run test:e2e
```

Validate Docker Compose configuration:

```bash
BASTIONGATE_API_KEY=validation-only docker compose config --quiet
```

Run the opt-in integration test against a real local BastionGate:

```bash
export BASTIONGATE_API_KEY="replace-with-local-api-client-key"
export BASTIONGATE_INTERNAL_URL="http://localhost:8080"
make test-live
```

## Troubleshooting

### The badge says disconnected

Check BastionGate health from the same network namespace as Acme People. When using Docker Desktop, the default internal URL is `http://host.docker.internal:8080`.

### Upload returns 401 or 403

Verify the API-client key and confirm that the client is authorized for `BASTIONGATE_POLICY_CODE`.

### EICAR is blocked before ClamAV evidence appears

Confirm the selected policy permits `.jpg` and `image/jpeg`. A policy rejection and a malware detection are separate decisions.

### The audit link does not work

Confirm `BASTIONGATE_PUBLIC_URL` is reachable from the browser. The BastionGate operator page may require an authenticated operator session.

### A clean image never becomes the avatar

Inspect the status and report. Acme People deliberately refuses the file unless the fresh BastionGate status is `RELEASED` and controlled download succeeds.

### Docker cannot reach host BastionGate

Use `host.docker.internal` on Docker Desktop. On supported Linux engines, Compose adds the matching host gateway. Alternatively, connect both services to a shared Docker network and use the BastionGate service name.

## Safety boundary

Use only the built-in EICAR option for the malicious-path demonstration. Do not replace it with webshells, executable payloads, or functional injection samples.
