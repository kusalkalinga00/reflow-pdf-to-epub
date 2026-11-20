"use strict";

const form = document.getElementById("convert-form");
const input = document.getElementById("file");
const drop = document.getElementById("drop");
const submit = document.getElementById("submit");
const cancel = document.getElementById("cancel");
const fileName = document.getElementById("file-name");
const status = document.getElementById("status");
const result = document.getElementById("result");
const stats = document.getElementById("stats");
const download = document.getElementById("download");
const issues = document.getElementById("issues");
const passwordRow = document.getElementById("password-row");
const passwordInput = document.getElementById("password");
const submitLabel = document.getElementById("submit-label");
const reportToggle = document.getElementById("report");
const progressWrap = document.getElementById("progress-wrap");
const progress = document.getElementById("progress");
const progressLabel = document.getElementById("progress-label");

let currentFile = null;
let currentJob = null;
let objectURL = null;
let startedAt = 0;
let cancelRequested = false;
let lastState = null;

function setStatus(message, tone) {
  status.textContent = message;
  status.className = "status";
  if (tone) {
    status.classList.add(tone);
  }
}

function formatSize(bytes) {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(0) + " KB";
  return (bytes / (1024 * 1024)).toFixed(1) + " MB";
}

function formatElapsed(ms) {
  const seconds = Math.max(0, Math.round(ms / 1000));
  const minutes = Math.floor(seconds / 60);
  return minutes + ":" + String(seconds % 60).padStart(2, "0");
}

function resetOutput() {
  result.hidden = true;
  if (objectURL) {
    URL.revokeObjectURL(objectURL);
    objectURL = null;
  }
  download.removeAttribute("href");
}

function resetProgress() {
  lastState = null;
  progressWrap.hidden = true;
  progress.removeAttribute("value");
  progressLabel.textContent = "";
}

function selectFile(file) {
  if (!file) return;

  const isPDF = file.type === "application/pdf" || /\.pdf$/i.test(file.name);
  if (!isPDF) {
    setStatus("That is not a PDF. Choose a file ending in .pdf.", "error");
    return;
  }

  currentFile = file;
  fileName.textContent = file.name + " · " + formatSize(file.size);
  fileName.hidden = false;
  submit.disabled = false;
  submitLabel.textContent = "Convert";
  passwordRow.hidden = true;
  passwordInput.value = "";
  resetOutput();
  resetProgress();
  setStatus("");
}

input.addEventListener("change", () => selectFile(input.files[0]));

["dragenter", "dragover"].forEach((event) => {
  drop.addEventListener(event, (e) => {
    e.preventDefault();
    drop.classList.add("over");
  });
});

["dragleave", "drop"].forEach((event) => {
  drop.addEventListener(event, (e) => {
    e.preventDefault();
    drop.classList.remove("over");
  });
});

drop.addEventListener("drop", (e) => {
  const file = e.dataTransfer.files[0];
  if (file) {
    // The hidden input cannot be populated, so keep the file in memory.
    selectFile(file);
  }
});

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!currentFile) return;

  submit.disabled = true;
  cancel.hidden = false;
  cancelRequested = false;
  resetOutput();
  resetProgress();
  progressWrap.hidden = false;
  progressLabel.textContent = "Uploading…";
  setStatus("Converting…");
  startedAt = Date.now();

  const body = new FormData();
  body.append("file", currentFile);
  if (!passwordRow.hidden && passwordInput.value) {
    body.append("password", passwordInput.value);
  }

  try {
    const response = await fetch("/api/jobs", { method: "POST", body });
    if (!response.ok) {
      throw await conversionError(response);
    }

    const started = await response.json();
    currentJob = started.id;
    await pollJob(started.id, started);
  } catch (err) {
    handleError(err);
  } finally {
    cancel.hidden = true;
    currentJob = null;
    submit.disabled = false;
  }
});

cancel.addEventListener("click", async () => {
  if (!currentJob) return;
  cancelRequested = true;
  cancel.disabled = true;
  try {
    await fetch("/api/jobs/" + currentJob, { method: "DELETE" });
  } catch {
    /* the job will expire on its own */
  } finally {
    cancel.disabled = false;
  }
});

async function pollJob(id, initial) {
  let state = initial;

  for (;;) {
    if (cancelRequested) {
      throw canceledError();
    }

    renderProgress(state);

    if (state.state === "done") {
      await finishJob(state);
      return;
    }
    if (state.state === "error") {
      const err = new Error(state.error || "Conversion failed.");
      err.code = state.code || "";
      throw err;
    }
    if (state.state === "canceled") {
      throw canceledError();
    }

    await delay(400);

    const response = await fetch("/api/jobs/" + id, { cache: "no-store" });
    if (!response.ok) {
      throw await conversionError(response);
    }
    state = await response.json();
  }
}

function renderProgress(state) {
  lastState = state;
  progressWrap.hidden = false;

  let label = "Starting…";
  if (state.state === "queued") {
    label = "Waiting for a free converter…";
  } else if (state.stage === "reading" && state.total > 0) {
    label = "Reading page " + state.done + " of " + state.total;
  } else if (state.stage === "reading") {
    label = "Opening the PDF…";
  } else if (state.stage === "laying out") {
    label = "Laying out pages…";
  } else if (state.stage === "writing") {
    label = "Writing the EPUB…";
  } else if (state.stage === "validating") {
    label = "Checking the EPUB…";
  } else if (state.stage === "done") {
    label = "Done.";
  }

  if (state.state === "done") {
    progress.value = 100;
    progress.textContent = "100%";
  } else if (state.total > 0) {
    const percent = Math.min(100, Math.round((100 * state.done) / state.total));
    progress.value = percent;
    progress.textContent = percent + "%";
  } else {
    // No page count yet: an indeterminate bar is the honest signal.
    progress.removeAttribute("value");
    progress.textContent = "";
  }

  progressLabel.textContent = label + " · " + formatElapsed(Date.now() - startedAt);
  setStatus("Converting…");
}

async function finishJob(state) {
  const response = await fetch("/api/jobs/" + state.id + "/epub");
  if (!response.ok) {
    throw await conversionError(response);
  }

  const blob = await response.blob();
  const report = state.quality || {};
  const tier = report.tier || "";
  const score = report.score || "";
  const showReport = !reportToggle || reportToggle.checked;

  objectURL = URL.createObjectURL(blob);
  download.href = objectURL;
  download.download = currentFile.name.replace(/\.pdf$/i, "") + ".epub";
  stats.textContent = state.pages + " pages · " + state.chapters + " chapters" +
    (showReport ? " · " + tierLabel(tier) + " " + score + "/100" : "") +
    " · " + formatSize(blob.size);

  issues.replaceChildren();
  const warnings = showReport
    ? (report.issues || []).filter((i) => i.severity === "warn")
    : [];
  for (const issue of warnings.slice(0, 3)) {
    const item = document.createElement("li");
    item.textContent = issue.message;
    issues.appendChild(item);
  }

  // The result card says everything the progress line would; leave the bar
  // behind so the finished state is one clean card.
  progressWrap.hidden = true;
  result.hidden = false;
  setStatus("Done. Your EPUB is ready.", "ok");
  download.focus();
}

function handleError(err) {
  if (err.canceled) {
    resetProgress();
    setStatus("Conversion canceled.");
    return;
  }

  if (err.code === "password") {
    // Ask instead of refusing: the reader already owns the password.
    const first = passwordRow.hidden;
    passwordRow.hidden = false;
    submitLabel.textContent = "Unlock";
    setStatus(err.message || "This PDF is password-protected.", "error");
    if (first) passwordInput.focus();
    progressWrap.hidden = true;
    return;
  }

  setStatus(err.message || "Conversion failed.", "error");
  if (lastState && lastState.state !== "done") {
    progressLabel.textContent = "Stopped.";
  }
}

function canceledError() {
  const err = new Error("Conversion canceled.");
  err.canceled = true;
  return err;
}

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function tierLabel(tier) {
  if (tier === "T1") return "Structured";
  if (tier === "T2") return "Reflowed";
  if (tier === "T3") return "Low confidence";
  return "Converted";
}

async function conversionError(response) {
  try {
    const body = await response.json();
    if (body && body.error) {
      const err = new Error(body.error);
      err.code = body.code || "";
      return err;
    }
  } catch {
    /* fall through to the generic message */
  }
  if (response.status === 413) return new Error("That file is too large.");
  return new Error("Conversion failed (" + response.status + ").");
}
