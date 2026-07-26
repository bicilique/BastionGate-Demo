# BastionGate Secure Upload Demo Implementation Plan

> **For agentic workers:** Execute each task as a vertical RED→GREEN slice. Do not batch all tests before implementation.

**Goal:** Build a Docker-runnable Acme People profile upload demo that sends untrusted files through local BastionGate and applies only released files.

**Architecture:** A single Go binary embeds the vanilla web UI and provides a same-origin BastionGate facade. Tests use public HTTP boundaries and a fake upstream server; a separate opt-in test covers real local ClamAV.

**Tech Stack:** Go 1.26, standard library HTTP, embedded HTML/CSS/JavaScript, Node 24 test runner, Playwright, Docker Compose.

## Global Constraints

- Keep `BASTIONGATE_API_KEY` server-side and send it upstream as a Bearer credential.
- Never store uploaded bytes in Acme People.
- Treat `202 Accepted` and all pending states as untrusted.
- Only `RELEASED` files may be downloaded and applied as an avatar.
- Generate EICAR at runtime; do not commit a standalone test file.
- Keep each RED→GREEN cycle focused on one externally observable behavior.

### Task 1: Upload tracer bullet

Create the Go module, configuration, BastionGate client, and HTTP server. First write a public HTTP test proving a multipart upload is limited, receives server-owned policy/correlation metadata, sends server-side Bearer auth, and returns an accepted-but-untrusted response. Run the focused test red, implement the minimum behavior, then run it green.

### Task 2: Trust decision and download gate

Add public status, report, and download facade routes one behavior at a time. Cover terminal-state classification, sensitive-field redaction, temporary error mapping, and the rule that download is refused unless a fresh status is `RELEASED`.

### Task 3: Guided web experience

Build the embedded enterprise-light UI and pure state helpers. Add the four approved stages, adaptive polling, report retry, runtime EICAR generation, clean-image release, redacted technical JSON, audit link, CTA, and reset behavior. Test state transitions first with Node's test runner, then cover blocked and released browser journeys.

### Task 4: Container and operator workflow

Add a multi-stage Dockerfile, Compose service, environment template, Make targets, and README covering BastionGate provisioning, Docker networking, safe EICAR usage, recording workflow, troubleshooting, and opt-in live verification.

### Task 5: Verification and review

Run formatting, `go vet`, all Go and JavaScript tests, browser tests, Docker build, Compose validation, and the live test when BastionGate is available. Review the complete diff against this design, fix findings, and commit the finished implementation to `demo-01`.

