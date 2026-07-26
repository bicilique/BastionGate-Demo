const TERMINAL_STATUSES = new Set([
  "RELEASED",
  "BLOCKED",
  "REVIEW_REQUIRED",
  "FAILED",
  "MALICIOUS",
  "FAILED_POLICY",
  "FAILED_SCAN",
]);
const TERMINAL_VERDICTS = new Set(["MALICIOUS", "FAILED_POLICY", "FAILED_SCAN"]);
const SUPPORTED_IMAGE_TYPES = new Set(["image/jpeg", "image/png"]);
const SUPPORTED_IMAGE_EXTENSION = /\.(?:jpe?g|png)$/i;

export function isTerminal(payload) {
  return (
    TERMINAL_STATUSES.has(payload?.status) ||
    TERMINAL_VERDICTS.has(payload?.finalVerdict)
  );
}

export function isSupportedImage(file) {
  if (!file) return false;
  return (
    SUPPORTED_IMAGE_TYPES.has(String(file.type).toLowerCase()) &&
    SUPPORTED_IMAGE_EXTENSION.test(String(file.name))
  );
}

export function isRetryableHTTPStatus(status) {
  return status === 429 || (status >= 500 && status <= 599);
}

export function boundedPollDelay(delay, remaining) {
  return Math.max(0, Math.min(delay, remaining));
}

export function isValidDemoConfig(config) {
  return (
    Number.isSafeInteger(config?.maxUploadBytes) &&
    config.maxUploadBytes > 0 &&
    typeof config?.policyCode === "string" &&
    config.policyCode.trim() !== ""
  );
}

export function isValidStatusPayload(payload) {
  return (
    payload !== null &&
    typeof payload === "object" &&
    (typeof payload.status === "string" || typeof payload.finalVerdict === "string")
  );
}

export function retryAfterMs(value, now = Date.now()) {
  if (typeof value === "string" && value.trim() !== "") {
    const seconds = Number(value);
    if (Number.isFinite(seconds) && seconds >= 0) {
      return seconds * 1000;
    }
  }

  const date = Date.parse(value);
  if (Number.isFinite(date)) {
    return Math.max(0, date - now);
  }
  return 1000;
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
  if (
    report?.status === "BLOCKED" ||
    report?.status === "MALICIOUS" ||
    report?.status === "FAILED_POLICY" ||
    report?.finalVerdict === "MALICIOUS" ||
    report?.finalVerdict === "FAILED_POLICY"
  ) {
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
  if (report?.status === "FAILED_SCAN" || report?.finalVerdict === "FAILED_SCAN") {
    return {
      kind: "failed",
      eyebrow: "FAILED SCAN · NOT RELEASED",
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
