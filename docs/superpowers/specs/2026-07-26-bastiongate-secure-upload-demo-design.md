# BastionGate Secure Upload Demo Design

## Purpose

Build a local, recording-friendly full-stack demo named **Acme People**. It presents an ordinary profile-photo upload while BastionGate performs quarantine-first intake, asynchronous scanning, trust evaluation, release control, and audit recording.

The demo must use the BastionGate app-client contract in `docs/postman/BastionGate.app-client.postman_collection.json` from the main BastionGate repository. It must never contain a webshell or functional exploit payload.

## Architecture

A single Go process serves embedded HTML, CSS, and JavaScript and exposes a same-origin API facade. The facade streams multipart uploads to BastionGate, adds the API-client Bearer credential, policy, source reference, and correlation ID, and exposes only the status, report, and controlled download operations required by the UI.

The browser never receives the BastionGate credential. The demo stores no files and has no database. BastionGate remains the source of truth for status, report, release, quarantine, and audit data.

Configuration:

- `BASTIONGATE_API_KEY` is required.
- `BASTIONGATE_INTERNAL_URL` defaults to `http://host.docker.internal:8080`.
- `BASTIONGATE_PUBLIC_URL` defaults to `http://localhost:8080`.
- `BASTIONGATE_POLICY_CODE` defaults to `DEFAULT`.
- `LISTEN_ADDR` defaults to `:3000`.
- Uploads are limited to 2 MiB.

## Experience

The English UI uses the approved enterprise-light visual direction and a four-stage guided flow:

1. Upload a user-selected JPG/PNG or generate the exact EICAR test bytes at runtime as `avatar.php.jpg`.
2. Show the accepted submission, redacted request evidence, file ID, correlation ID, and live pending status.
3. Show the final Trust Decision. A blocked EICAR flow highlights `MALICIOUS`, ClamAV, and the detected signature. A released clean image becomes the avatar only after controlled download succeeds.
4. Show the redacted technical report, link to `${BASTIONGATE_PUBLIC_URL}/files/{fileId}`, closing CTA, and reset action.

The runtime EICAR action includes a visible disclaimer that it is harmless, contains no executable code, and is intended for antivirus testing. No standalone EICAR file is committed.

## Failure behavior

Missing credentials fail startup. An unavailable BastionGate leaves the UI reachable but disconnected and disables upload. Polling begins at one-second intervals, honors `Retry-After`, retries temporary network/5xx failures, and stops after 60 seconds with a retry action that preserves the file ID.

The facade returns safe problem JSON for invalid input, oversized uploads, authentication/authorization failures, upstream timeouts, and malformed upstream responses. Credentials and storage-location fields are recursively removed from data shown in the technical panel.

## Testing

Development follows vertical-slice TDD. Go integration tests exercise the public HTTP facade against an `httptest` BastionGate boundary. Browser tests exercise blocked and released journeys through the rendered UI. An opt-in live test generates EICAR at runtime and verifies the real local ClamAV result without making the normal suite depend on BastionGate.

