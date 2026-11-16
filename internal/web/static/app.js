"use strict";

const form = document.getElementById("convert-form");
const input = document.getElementById("file");
const drop = document.getElementById("drop");
const submit = document.getElementById("submit");
const fileName = document.getElementById("file-name");
const status = document.getElementById("status");
const result = document.getElementById("result");
const stats = document.getElementById("stats");
const download = document.getElementById("download");

let currentFile = null;
let objectURL = null;

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

function resetOutput() {
  result.hidden = true;
  if (objectURL) {
    URL.revokeObjectURL(objectURL);
    objectURL = null;
  }
  download.removeAttribute("href");
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
  resetOutput();
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
  resetOutput();
  setStatus("Converting… this can take a few seconds for a long book.");

  const body = new FormData();
  body.append("file", currentFile);

  try {
    const response = await fetch("/api/convert", { method: "POST", body });
    if (!response.ok) {
      throw new Error(await errorMessage(response));
    }

    const blob = await response.blob();
    const pages = response.headers.get("X-Reflow-Pages") || "?";
    const chapters = response.headers.get("X-Reflow-Chapters") || "?";

    objectURL = URL.createObjectURL(blob);
    download.href = objectURL;
    download.download = currentFile.name.replace(/\.pdf$/i, "") + ".epub";
    stats.textContent = pages + " pages · " + chapters + " chapters · " + formatSize(blob.size);

    result.hidden = false;
    setStatus("Done. Your EPUB is ready.", "ok");
    download.focus();
  } catch (err) {
    setStatus(err.message || "Conversion failed.", "error");
  } finally {
    submit.disabled = false;
  }
});

async function errorMessage(response) {
  try {
    const body = await response.json();
    if (body && body.error) return body.error;
  } catch {
    /* fall through to the generic message */
  }
  if (response.status === 413) return "That file is too large.";
  return "Conversion failed (" + response.status + ").";
}
