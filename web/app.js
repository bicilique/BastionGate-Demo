import {
  boundedPollDelay,
  decisionView,
  eicarText,
  isRetryableHTTPStatus,
  isSupportedImage,
  isTerminal,
  isValidDemoConfig,
  isValidStatusPayload,
  retryAfterMs,
} from "./app-state.js";

const MAX_UPLOAD_BYTES = 2 * 1024 * 1024;
const POLL_DEADLINE_MS = 60_000;
const DEFAULT_POLL_MS = 1_000;

const $ = (selector) => document.querySelector(selector);
const elements = {
  connectionBadge: $("#connectionBadge"),
  connectionLabel: $("#connectionLabel"),
  uploadLimit: $("#uploadLimit"),
  fileInput: $("#fileInput"),
  dropZone: $("#dropZone"),
  eicarButton: $("#eicarButton"),
  uploadButton: $("#uploadButton"),
  selectedFile: $("#selectedFile"),
  selectedFileName: $("#selectedFileName"),
  selectedFileMeta: $("#selectedFileMeta"),
  errorNotice: $("#errorNotice"),
  errorTitle: $("#errorTitle"),
  errorMessage: $("#errorMessage"),
  dismissError: $("#dismissError"),
  requestEvidence: $("#requestEvidence"),
  acceptedEvidence: $("#acceptedEvidence"),
  liveStatus: $("#liveStatus"),
  timeoutPanel: $("#timeoutPanel"),
  retryStatus: $("#retryStatus"),
  decisionCard: $("#decisionCard"),
  decisionSymbol: $("#decisionSymbol"),
  decisionEyebrow: $("#decisionEyebrow"),
  decisionTitle: $("#decisionTitle"),
  decisionSummary: $("#decisionSummary"),
  decisionFileName: $("#decisionFileName"),
  decisionEngine: $("#decisionEngine"),
  decisionFileID: $("#decisionFileID"),
  continueAudit: $("#continueAudit"),
  retryReport: $("#retryReport"),
  auditLink: $("#auditLink"),
  toggleReport: $("#toggleReport"),
  technicalReport: $("#technicalReport"),
  reportSummary: $("#reportSummary"),
  rawReport: $("#rawReport"),
  resetDemo: $("#resetDemo"),
  releasedAvatar: $("#releasedAvatar"),
  avatarInitials: $("#avatarInitials"),
};

const session = {
  connected: false,
  selectedFile: null,
  accepted: null,
  status: null,
  report: null,
  avatarURL: null,
  pollGeneration: 0,
  config: {
    maxUploadBytes: MAX_UPLOAD_BYTES,
    policyCode: "DEFAULT",
  },
};

function setStage(number) {
  document.querySelectorAll("[data-stage]").forEach((stage) => {
    const active = Number(stage.dataset.stage) === number;
    stage.hidden = !active;
    stage.classList.toggle("active", active);
  });
  document.querySelectorAll("[data-step-indicator]").forEach((step) => {
    const value = Number(step.dataset.stepIndicator);
    step.classList.toggle("active", value === number);
    step.classList.toggle("complete", value < number);
  });
  window.scrollTo({ top: 0, behavior: "smooth" });
}

function setConnection(connected) {
  session.connected = connected;
  elements.connectionBadge.dataset.state = connected ? "connected" : "disconnected";
  elements.connectionLabel.textContent = connected
    ? "BastionGate connected"
    : "BastionGate disconnected";
  refreshUploadButton();
}

async function checkConnection() {
  elements.connectionBadge.dataset.state = "checking";
  elements.connectionLabel.textContent = "Checking BastionGate…";
  try {
    const [healthResponse, configResponse] = await Promise.all([
      fetch("/api/health", { cache: "no-store" }),
      fetch("/api/config", { cache: "no-store" }),
    ]);
    if (!healthResponse.ok || !configResponse.ok) {
      setConnection(false);
      return;
    }
    const config = await configResponse.json();
    if (!isValidDemoConfig(config)) {
      setConnection(false);
      return;
    }
    session.config = config;
    elements.uploadLimit.textContent = `${humanSize(session.config.maxUploadBytes)} max`;
    setConnection(true);
  } catch {
    setConnection(false);
  }
}

function refreshUploadButton() {
  elements.uploadButton.disabled = !session.connected || !session.selectedFile;
}

function humanSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes >= 1024 * 1024) {
    return `${(bytes / (1024 * 1024)).toFixed(1).replace(".0", "")} MiB`;
  }
  return `${(bytes / 1024).toFixed(1)} KiB`;
}

function selectFile(file) {
  hideError();
  if (!file) return;
  if (!isSupportedImage(file)) {
    showError("Unsupported file type", "Choose a JPG or PNG profile photo.");
    return;
  }
  if (file.size > session.config.maxUploadBytes) {
    showError(
      "File is too large",
      `Profile photos are limited to ${humanSize(session.config.maxUploadBytes)}.`,
    );
    return;
  }
  session.selectedFile = file;
  elements.selectedFileName.textContent = file.name;
  elements.selectedFileMeta.textContent = `${humanSize(file.size)} · ${file.type || "unknown type"}`;
  elements.selectedFile.hidden = false;
  refreshUploadButton();
}

function createEicarDemoFile() {
  return new File([eicarText()], "avatar.php.jpg", {
    type: "image/jpeg",
    lastModified: Date.now(),
  });
}

function showError(title, message) {
  elements.errorTitle.textContent = title;
  elements.errorMessage.textContent = message;
  elements.errorNotice.hidden = false;
}

function hideError() {
  elements.errorNotice.hidden = true;
}

async function readProblem(response) {
  try {
    const body = await response.json();
    return body.message || body.code || `Request failed with HTTP ${response.status}.`;
  } catch {
    return `Request failed with HTTP ${response.status}.`;
  }
}

async function uploadSelectedFile() {
  if (!session.selectedFile || !session.connected) return;
  hideError();
  elements.uploadButton.disabled = true;
  elements.uploadButton.textContent = "Sending to BastionGate…";

  const form = new FormData();
  form.set("file", session.selectedFile);
  try {
    const response = await fetch("/api/uploads", { method: "POST", body: form });
    if (!response.ok) throw new Error(await readProblem(response));
    session.accepted = await response.json();
    renderAccepted();
    setStage(2);
    await pollUntilFinal();
  } catch (error) {
    showError("Upload was not accepted", error.message);
    elements.uploadButton.textContent = "Send through BastionGate";
    refreshUploadButton();
  }
}

function renderAccepted() {
  const { accepted } = session;
  elements.requestEvidence.textContent = [
    "POST /api/v1/files/upload",
    "Authorization: Bearer ••••REDACTED••••",
    `X-Correlation-ID: ${accepted.correlationId}`,
    `file=@${session.selectedFile.name}`,
    `policyCode=${session.config.policyCode}`,
  ].join("\n");
  elements.acceptedEvidence.textContent = JSON.stringify(
    {
      fileId: accepted.fileId,
      sourceType: accepted.sourceType,
      status: accepted.status,
      trust: accepted.trust,
    },
    null,
    2,
  );
  updateLifecycle(accepted.status || "QUEUED");
}

function sleep(milliseconds) {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds));
}

async function pollUntilFinal() {
  const generation = ++session.pollGeneration;
  const deadline = Date.now() + POLL_DEADLINE_MS;
  elements.timeoutPanel.hidden = true;

  while (generation === session.pollGeneration && Date.now() < deadline) {
    const controller = new AbortController();
    const abortTimer = window.setTimeout(
      () => controller.abort(),
      Math.max(0, deadline - Date.now()),
    );
    let response;
    try {
      response = await fetch(`/api/files/${encodeURIComponent(session.accepted.fileId)}/status`, {
        cache: "no-store",
        signal: controller.signal,
      });
    } catch (error) {
      if (controller.signal.aborted || Date.now() >= deadline) {
        break;
      }
      await sleep(boundedPollDelay(DEFAULT_POLL_MS, deadline - Date.now()));
      continue;
    } finally {
      window.clearTimeout(abortTimer);
    }

    if (!response.ok) {
      const message = await readProblem(response);
      if (!isRetryableHTTPStatus(response.status)) {
        showError("Status polling stopped", message);
        return;
      }
      const delay =
        response.status === 429
          ? retryAfterMs(response.headers.get("Retry-After"))
          : DEFAULT_POLL_MS;
      await sleep(boundedPollDelay(delay, deadline - Date.now()));
      continue;
    }

    try {
      session.status = await response.json();
    } catch {
      showError("Status polling stopped", "BastionGate returned invalid status JSON.");
      return;
    }
    if (!isValidStatusPayload(session.status)) {
      showError("Status polling stopped", "BastionGate returned an invalid status payload.");
      return;
    }
    updateLifecycle(session.status.status);
    if (isTerminal(session.status)) {
      await loadReport();
      return;
    }
    await sleep(boundedPollDelay(DEFAULT_POLL_MS, deadline - Date.now()));
  }
  if (generation === session.pollGeneration) elements.timeoutPanel.hidden = false;
}

function updateLifecycle(status) {
  elements.liveStatus.textContent = status || "QUEUED";
  const order = ["RECEIVED", "QUARANTINED", "QUEUED", "SCANNING"];
  const activeIndex = Math.max(0, order.indexOf(status));
  document.querySelectorAll(".lifecycle-item").forEach((item, index) => {
    const final = item.dataset.status === "FINAL";
    item.classList.toggle("complete", isTerminal({ status }) || (!final && index < activeIndex));
    item.classList.toggle("active", (final && isTerminal({ status })) || (!final && index === activeIndex));
  });
}

async function loadReport() {
  elements.retryReport.hidden = true;
  try {
    const response = await fetch(`/api/files/${encodeURIComponent(session.accepted.fileId)}/report`, {
      cache: "no-store",
    });
    if (!response.ok) throw new Error(await readProblem(response));
    session.report = await response.json();
    await renderDecision();
    setStage(3);
  } catch (error) {
    session.report = {
      fileId: session.accepted.fileId,
      fileName: session.selectedFile.name,
      status: session.status?.status || "FAILED",
      finalVerdict: session.status?.finalVerdict || "UNKNOWN",
    };
    renderDecisionFallback(error.message);
    setStage(3);
  }
}

function findEngine(report) {
  const engines = report?.engines || report?.scanEngines || report?.scan?.engines || [];
  return engines.find((engine) => String(engine.engine || engine.name).toUpperCase() === "CLAMAV");
}

async function renderDecision() {
  const view = decisionView(session.report);
  const engine = findEngine(session.report);
  elements.decisionCard.dataset.kind = view.kind;
  elements.decisionSymbol.textContent = view.kind === "released" ? "✓" : view.kind === "blocked" ? "!" : "·";
  elements.decisionEyebrow.textContent = view.eyebrow;
  elements.decisionTitle.textContent = view.title;
  elements.decisionSummary.textContent = view.summary;
  elements.decisionFileName.textContent = session.report.fileName || session.selectedFile.name;
  elements.decisionEngine.textContent = engine
    ? `${engine.engine || engine.name} · ${engine.threatName || engine.result || "completed"}`
    : session.report.engineVerdict || "BastionGate policy";
  elements.decisionFileID.textContent = session.accepted.fileId;
  if (view.kind === "released") await applyReleasedAvatar();
}

function renderDecisionFallback(message) {
  const view = decisionView(session.report);
  elements.decisionCard.dataset.kind = view.kind;
  elements.decisionSymbol.textContent = "·";
  elements.decisionEyebrow.textContent = view.eyebrow;
  elements.decisionTitle.textContent = view.title;
  elements.decisionSummary.textContent = `${view.summary} Technical report unavailable: ${message}`;
  elements.decisionFileName.textContent = session.selectedFile.name;
  elements.decisionEngine.textContent = "Report unavailable";
  elements.decisionFileID.textContent = session.accepted.fileId;
  elements.retryReport.hidden = false;
}

async function applyReleasedAvatar() {
  try {
    const response = await fetch(`/api/files/${encodeURIComponent(session.accepted.fileId)}/download`, {
      cache: "no-store",
    });
    if (!response.ok) throw new Error(await readProblem(response));
    const blob = await response.blob();
    if (session.avatarURL) URL.revokeObjectURL(session.avatarURL);
    session.avatarURL = URL.createObjectURL(blob);
    elements.releasedAvatar.src = session.avatarURL;
    elements.releasedAvatar.hidden = false;
    elements.avatarInitials.hidden = true;
  } catch (error) {
    showError("Released photo could not be loaded", error.message);
  }
}

function renderAudit() {
  const report = session.report || {};
  elements.auditLink.hidden = !report.auditUrl;
  if (report.auditUrl) elements.auditLink.href = report.auditUrl;
  elements.rawReport.textContent = JSON.stringify(report, null, 2);
  const engine = findEngine(report);
  const facts = [
    ["Status", report.status || session.status?.status || "UNKNOWN"],
    ["Verdict", report.finalVerdict || session.status?.finalVerdict || "UNKNOWN"],
    ["Policy", report.policyCode || session.config.policyCode],
    ["Engine", engine?.engine || engine?.name || "—"],
    ["Signature", engine?.threatName || engine?.result || "—"],
    ["File ID", session.accepted.fileId],
  ];
  elements.reportSummary.replaceChildren(
    ...facts.map(([term, value]) => {
      const wrapper = document.createElement("div");
      const dt = document.createElement("dt");
      const dd = document.createElement("dd");
      dt.textContent = term;
      dd.textContent = value;
      wrapper.append(dt, dd);
      return wrapper;
    }),
  );
}

function resetDemo() {
  session.pollGeneration += 1;
  session.selectedFile = null;
  session.accepted = null;
  session.status = null;
  session.report = null;
  elements.fileInput.value = "";
  elements.selectedFile.hidden = true;
  elements.timeoutPanel.hidden = true;
  elements.technicalReport.hidden = true;
  elements.uploadButton.textContent = "Send through BastionGate";
  hideError();
  refreshUploadButton();
  setStage(1);
}

elements.fileInput.addEventListener("change", () => selectFile(elements.fileInput.files[0]));
elements.eicarButton.addEventListener("click", () => selectFile(createEicarDemoFile()));
elements.uploadButton.addEventListener("click", uploadSelectedFile);
elements.dismissError.addEventListener("click", hideError);
elements.retryStatus.addEventListener("click", pollUntilFinal);
elements.retryReport.addEventListener("click", loadReport);
elements.continueAudit.addEventListener("click", () => { renderAudit(); setStage(4); });
elements.toggleReport.addEventListener("click", () => {
  elements.technicalReport.hidden = !elements.technicalReport.hidden;
  elements.toggleReport.textContent = elements.technicalReport.hidden
    ? "View technical report"
    : "Hide technical report";
});
elements.resetDemo.addEventListener("click", resetDemo);

for (const eventName of ["dragenter", "dragover"]) {
  elements.dropZone.addEventListener(eventName, (event) => {
    event.preventDefault();
    elements.dropZone.classList.add("dragging");
  });
}
for (const eventName of ["dragleave", "drop"]) {
  elements.dropZone.addEventListener(eventName, (event) => {
    event.preventDefault();
    elements.dropZone.classList.remove("dragging");
  });
}
elements.dropZone.addEventListener("drop", (event) => selectFile(event.dataTransfer.files[0]));

document.querySelectorAll("[data-report-tab]").forEach((tab) => {
  tab.addEventListener("click", () => {
    document.querySelectorAll("[data-report-tab]").forEach((candidate) => {
      candidate.classList.toggle("active", candidate === tab);
    });
    document.querySelectorAll("[data-report-panel]").forEach((panel) => {
      panel.hidden = panel.dataset.reportPanel !== tab.dataset.reportTab;
    });
  });
});

checkConnection();
