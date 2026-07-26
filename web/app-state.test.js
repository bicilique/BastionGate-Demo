import assert from "node:assert/strict";
import test from "node:test";

import {
  boundedPollDelay,
  decisionView,
  isRetryableHTTPStatus,
  isSupportedImage,
  isTerminal,
  isValidDemoConfig,
  isValidStatusPayload,
  retryAfterMs,
} from "./app-state.js";

test("only final BastionGate file states end polling", () => {
  for (const status of ["RELEASED", "BLOCKED", "REVIEW_REQUIRED", "FAILED"]) {
    assert.equal(isTerminal({ status }), true, status);
  }

  for (const status of ["RECEIVED", "QUARANTINED", "QUEUED", "SCANNING"]) {
    assert.equal(isTerminal({ status }), false, status);
  }
});

test("terminal verdicts end polling even when status has not caught up", () => {
  assert.equal(isTerminal({ status: "SCANNING", finalVerdict: "MALICIOUS" }), true);
  assert.equal(isTerminal({ status: "SCANNING", finalVerdict: "FAILED_SCAN" }), true);
  assert.equal(isTerminal({ status: "SCANNING" }), false);
});

test("Postman terminal values also work when returned as status", () => {
  assert.equal(isTerminal({ status: "MALICIOUS" }), true);
  assert.equal(isTerminal({ status: "FAILED_SCAN" }), true);
  assert.equal(decisionView({ status: "MALICIOUS" }).kind, "blocked");
  assert.equal(decisionView({ status: "FAILED_SCAN" }).kind, "failed");
});

test("profile uploads require a supported image type and extension", () => {
  assert.equal(isSupportedImage({ name: "avatar.php.jpg", type: "image/jpeg" }), true);
  assert.equal(isSupportedImage({ name: "profile.png", type: "image/png" }), true);
  assert.equal(isSupportedImage({ name: "notes.txt", type: "text/plain" }), false);
  assert.equal(isSupportedImage({ name: "photo.jpg", type: "text/plain" }), false);
});

test("malicious report produces a blocked decision with ClamAV evidence", () => {
  const decision = decisionView({
    status: "BLOCKED",
    finalVerdict: "MALICIOUS",
    engines: [
      {
        engine: "CLAMAV",
        threatName: "Test-Signature",
      },
    ],
  });

  assert.deepEqual(decision, {
    kind: "blocked",
    eyebrow: "MALICIOUS · BLOCKED",
    title: "Your photo was blocked",
    summary: "ClamAV detected Test-Signature. The file was not released to Acme People.",
  });
});

test("released report explicitly permits the profile photo", () => {
  assert.deepEqual(
    decisionView({ status: "RELEASED", finalVerdict: "CLEAN" }),
    {
      kind: "released",
      eyebrow: "CLEAN · RELEASED",
      title: "Your photo is ready",
      summary: "BastionGate released this file for use by Acme People.",
    },
  );
});

test("review and failed decisions remain conservative", () => {
  assert.equal(
    decisionView({ status: "REVIEW_REQUIRED", finalVerdict: "SUSPICIOUS" }).kind,
    "review",
  );
  assert.equal(
    decisionView({ status: "FAILED", finalVerdict: "FAILED_SCAN" }).kind,
    "failed",
  );
});

test("polling retries only temporary HTTP failures", () => {
  for (const status of [429, 500, 502, 503, 504]) {
    assert.equal(isRetryableHTTPStatus(status), true, status);
  }
  for (const status of [400, 401, 403, 404]) {
    assert.equal(isRetryableHTTPStatus(status), false, status);
  }
});

test("browser config and status payloads must match their contracts", () => {
  assert.equal(isValidDemoConfig({ maxUploadBytes: 2_097_152, policyCode: "DEFAULT" }), true);
  assert.equal(isValidDemoConfig({ maxUploadBytes: 0, policyCode: "DEFAULT" }), false);
  assert.equal(isValidDemoConfig({ maxUploadBytes: 2_097_152, policyCode: "" }), false);
  assert.equal(isValidStatusPayload({ status: "SCANNING" }), true);
  assert.equal(isValidStatusPayload({ finalVerdict: "MALICIOUS" }), true);
  assert.equal(isValidStatusPayload({ fileId: "missing-state" }), false);
});

test("Retry-After supports seconds and HTTP dates", () => {
  const now = Date.parse("2026-07-26T12:00:00Z");
  assert.equal(retryAfterMs("3", now), 3_000);
  assert.equal(retryAfterMs("Sun, 26 Jul 2026 12:00:05 GMT", now), 5_000);
  assert.equal(retryAfterMs(null, now), 1_000);
  assert.equal(retryAfterMs("", now), 1_000);
  assert.equal(retryAfterMs("invalid", now), 1_000);
  assert.equal(boundedPollDelay(120_000, 4_000), 4_000);
});
