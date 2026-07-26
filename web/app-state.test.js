import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import test from "node:test";

import { decisionView, eicarText, isTerminal } from "./app-state.js";

test("only final BastionGate file states end polling", () => {
  for (const status of ["RELEASED", "BLOCKED", "REVIEW_REQUIRED", "FAILED"]) {
    assert.equal(isTerminal({ status }), true, status);
  }

  for (const status of ["RECEIVED", "QUARANTINED", "QUEUED", "SCANNING"]) {
    assert.equal(isTerminal({ status }), false, status);
  }
});

test("runtime EICAR content is the exact harmless 68-byte standard", () => {
  const content = eicarText();

  assert.equal(Buffer.byteLength(content, "ascii"), 68);
  assert.equal(
    createHash("sha256").update(content, "ascii").digest("hex"),
    "275a021bbfb6489e54d471899f7db9d1663fc695ec2fe2a2c4538aabf651fd0f",
  );
});

test("malicious report produces a blocked decision with ClamAV evidence", () => {
  const decision = decisionView({
    status: "BLOCKED",
    finalVerdict: "MALICIOUS",
    engines: [
      {
        engine: "CLAMAV",
        threatName: "Eicar-Signature",
      },
    ],
  });

  assert.deepEqual(decision, {
    kind: "blocked",
    eyebrow: "MALICIOUS · BLOCKED",
    title: "Your photo was blocked",
    summary: "ClamAV detected Eicar-Signature. The file was not released to Acme People.",
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
