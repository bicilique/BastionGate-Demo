const TERMINAL_STATUSES = new Set([
  "RELEASED",
  "BLOCKED",
  "REVIEW_REQUIRED",
  "FAILED",
]);

export function isTerminal(payload) {
  return TERMINAL_STATUSES.has(payload?.status);
}

export function eicarText() {
  return [
    "X5O!P%@AP[4",
    "\\PZX54(P^)7CC)7}$",
    "EICAR-STANDARD-ANTIVIRUS-TEST-FILE",
    "!$H+H*",
  ].join("");
}

export function decisionView(report) {
  if (report?.status === "RELEASED") {
    return {
      kind: "released",
      eyebrow: `${report?.finalVerdict || "CLEAN"} · RELEASED`,
      title: "Your photo is ready",
      summary: "BastionGate released this file for use by Acme People.",
    };
  }
  if (report?.status === "BLOCKED" || report?.finalVerdict === "MALICIOUS") {
    const clamAV = report?.engines?.find(
      (engine) => String(engine.engine ?? engine.name).toUpperCase() === "CLAMAV",
    );
    const threatName = clamAV?.threatName || "a malicious signature";
    return {
      kind: "blocked",
      eyebrow: `${report?.finalVerdict || "BLOCKED"} · BLOCKED`,
      title: "Your photo was blocked",
      summary: `ClamAV detected ${threatName}. The file was not released to Acme People.`,
    };
  }
  if (report?.status === "REVIEW_REQUIRED") {
    return {
      kind: "review",
      eyebrow: `${report?.finalVerdict || "SUSPICIOUS"} · REVIEW REQUIRED`,
      title: "Your photo needs review",
      summary: "Acme People will keep the current profile photo until BastionGate completes human review.",
    };
  }
  if (report?.status === "FAILED") {
    return {
      kind: "failed",
      eyebrow: `${report?.finalVerdict || "FAILED"} · NOT RELEASED`,
      title: "Your photo could not be verified",
      summary: "The file remains untrusted and was not released to Acme People.",
    };
  }
  return {
    kind: "pending",
    eyebrow: "TRUST DECISION PENDING",
    title: "BastionGate is still checking this file",
    summary: "The current profile photo remains unchanged.",
  };
}
