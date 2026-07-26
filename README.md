# BastionGate Secure Upload Demo

Acme People is a fictional profile application that demonstrates an ordinary upload endpoint protected by a locally running BastionGate. The browser sends an Untrusted Upload to the Acme People facade, the facade streams it to BastionGate, and the application waits for a final Trust Decision before it can use the file.

The built-in malicious-path demo uses only the harmless, industry-standard EICAR antivirus test file. It contains no executable code, webshell, or functional exploit payload.

## What the demo shows

1. Select a JPG/PNG or generate `avatar.php.jpg` from the standard EICAR bytes at runtime.
2. Acme People forwards the multipart upload to `POST /api/v1/files/upload`.
3. The UI treats `202 Accepted` as untrusted and polls `/api/v1/files/{fileId}/status`.
4. BastionGate reports a final decision and the UI retrieves the redacted trust report.
5. Only `RELEASED` files can be downloaded and applied as the profile photo.
6. The audit action opens the real BastionGate `/files/{fileId}` operator page.

No upload is stored by Acme People. The API-client key remains in the Go process and never enters browser HTML, JavaScript, storage, or network requests.

## Prerequisites

- A local BastionGate stack reachable on `http://localhost:8080`.
- A BastionGate API-client credential authorized for the active `DEFAULT` policy.
- BastionGate scan worker, ClamAV, quarantine storage, and PostgreSQL running.
- Docker with Compose v2.

The API behavior follows the app-client Postman collection in the main BastionGate repository:

```text
docs/postman/BastionGate.app-client.postman_collection.json
```

## Run with Docker

Create local configuration and set the API-client key:

```bash
cp .env.example .env
$EDITOR .env
```

Start the demo:

```bash
docker compose up --build
```

Open:

- Acme People: <http://localhost:3000>
- BastionGate operator UI: <http://localhost:8080>

On Docker Desktop, `host.docker.internal` reaches BastionGate running on the host. The Compose `extra_hosts` entry provides the same name on supported Linux Docker engines. If both applications share a user-defined Docker network, set `BASTIONGATE_INTERNAL_URL` to the BastionGate service URL instead.

## Record the EICAR journey

1. Confirm the header says **BastionGate connected**.
2. Click **Use the EICAR demo file**. The screen shows `avatar.php.jpg`, 68 B, and `UNTRUSTED`.
3. Click **Send through BastionGate**.
4. Record the accepted request, correlation ID, queue/scan progress, and final ClamAV decision.
5. Click **Continue to audit**.
6. Open **BastionGate file audit** in a new tab to show the real lifecycle and operator evidence.
7. Return to Acme People for the closing `bastiongate.tech` CTA.

Some endpoint-security products may intentionally alert when the browser constructs or transmits EICAR. That alert is expected scanner behavior. Do not replace EICAR with a real payload.

## Run without Docker

Go 1.26 or newer is required:

```bash
export BASTIONGATE_API_KEY="replace-with-local-api-client-key"
export BASTIONGATE_INTERNAL_URL="http://localhost:8080"
export BASTIONGATE_PUBLIC_URL="http://localhost:8080"
go run ./cmd/acme-people
```

Then open <http://localhost:3000>.

## Verification

Run the normal suite without requiring BastionGate:

```bash
make test
go vet ./...
```

Run the browser journeys:

```bash
npm install
npx playwright install chromium
make test-e2e
```

Run the opt-in test against the real local BastionGate:

```bash
export BASTIONGATE_API_KEY="replace-with-local-api-client-key"
export BASTIONGATE_INTERNAL_URL="http://localhost:8080"
make test-live
```

Validate container packaging:

```bash
make docker-build
BASTIONGATE_API_KEY=validation-only make docker-config
```

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `BASTIONGATE_API_KEY` | required | Server-side API-client credential |
| `BASTIONGATE_INTERNAL_URL` | `http://host.docker.internal:8080` | URL used by the Go facade |
| `BASTIONGATE_PUBLIC_URL` | `http://localhost:8080` | Browser-facing operator UI URL |
| `BASTIONGATE_POLICY_CODE` | `DEFAULT` | Policy added to upload intake |
| `LISTEN_ADDR` | `:3000` | Go HTTP listen address |
| `ACME_PEOPLE_PORT` | `3000` | Docker host port |

## Troubleshooting

- **Disconnected badge:** verify BastionGate health at `/actuator/health` from the same network namespace as Acme People.
- **401/403:** provision an API client and authorize it for the configured policy. The focused Postman collection uses `Authorization: Bearer <apiClientKey>`.
- **429:** the UI honors `Retry-After` and continues polling within its 60-second window.
- **Blocked by policy before release:** confirm `DEFAULT` accepts `.jpg` and the API client is allowed to use it. ClamAV scanning still provides the EICAR evidence in the report.
- **Audit link opens login:** authenticate as a BastionGate operator; Acme People intentionally stores no operator credentials.
- **File never appears as avatar:** this is correct unless BastionGate returns `RELEASED` and its controlled download succeeds.

