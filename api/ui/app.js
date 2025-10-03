const messagesEl = document.getElementById("messages");
const formEl = document.getElementById("composer");
const textareaEl = document.getElementById("question");
const sendBtn = document.getElementById("send");
const resetBtn = document.getElementById("reset");
const uploadForm = document.getElementById("upload-form");
const fileInput = document.getElementById("document");
const ingestBtn = document.getElementById("ingest");
const ingestStatusEl = document.getElementById("ingest-status");

let conversationHistory = [];
let streaming = false;
let currentAssistant = null;
let ingesting = false;

formEl.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (streaming) {
    return;
  }

  const question = textareaEl.value.trim();
  if (!question) {
    return;
  }

  appendUserMessage(question);
  textareaEl.value = "";
  textareaEl.style.height = "auto";

  currentAssistant = appendAssistantMessage();
  setStreamingState(true);

  try {
    await streamChat(question);
  } catch (error) {
    showError(error);
  } finally {
    setStreamingState(false);
  }
});

textareaEl.addEventListener("input", () => {
  textareaEl.style.height = "auto";
  textareaEl.style.height = `${textareaEl.scrollHeight}px`;
});

resetBtn.addEventListener("click", () => {
  if (streaming) {
    return;
  }
  conversationHistory = [];
  messagesEl.innerHTML = "";
  currentAssistant = null;
  textareaEl.value = "";
  textareaEl.focus();
});

if (uploadForm && fileInput && ingestBtn) {
  uploadForm.addEventListener("submit", async (event) => {
    event.preventDefault();
    if (ingesting) {
      return;
    }
    const file = fileInput.files && fileInput.files[0];
    if (!file) {
      showUploadError(new Error("Select a .md, .pdf, or .csv file to ingest."));
      return;
    }
    try {
      await ingestDocument(file);
    } catch (error) {
      showUploadError(error);
    }
  });

  fileInput.addEventListener("change", () => {
    if (!fileInput.files || fileInput.files.length === 0) {
      updateIngestStatus("");
      return;
    }
    const file = fileInput.files[0];
    updateIngestStatus(`${file.name}`);
  });
}

async function streamChat(question) {
  const payload = {
    question,
    history: conversationHistory,
  };

  const response = await fetch("/v1/chat/stream", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });

  if (!response.ok || !response.body) {
    const message = await response.text();
    throw new Error(message || response.statusText);
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  while (true) {
    const { value, done } = await reader.read();
    if (done) {
      break;
    }
    buffer += decoder.decode(value, { stream: true });

    let boundary = buffer.indexOf("\n\n");
    while (boundary !== -1) {
      const rawEvent = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      if (rawEvent.trim() !== "") {
        handleSSEChunk(rawEvent);
      }
      boundary = buffer.indexOf("\n\n");
    }
  }
}

async function ingestDocument(file) {
  const formData = new FormData();
  formData.append("document", file);

  setIngestState(true, `Uploading ${file.name}…`);

  try {
    const response = await fetch("/v1/ingest/upload", {
      method: "POST",
      body: formData,
    });

    let payload;
    try {
      payload = await response.json();
    } catch (parseError) {
      if (!response.ok) {
        throw new Error(`Upload failed (${response.status})`);
      }
      throw new Error("Unexpected response from server");
    }

    if (!response.ok) {
      throw new Error(payload?.error || `Upload failed (${response.status})`);
    }

    const chunks = typeof payload?.document?.chunks === "number" ? payload.document.chunks : null;
    const title = payload?.document?.title || file.name;
    let message = payload?.message || `Ingested ${title}`;
    if (chunks && chunks > 0) {
      const label = chunks === 1 ? "chunk" : "chunks";
      message = `${message} (${chunks} ${label})`;
    }

    setIngestState(false, message);
    fileInput.value = "";
    ingestBtn.blur();
  } catch (error) {
    setIngestState(false);
    throw error;
  }
}

function handleSSEChunk(chunk) {
  const lines = chunk.split("\n");
  let eventType = "message";
  const dataLines = [];

  for (const line of lines) {
    if (line.startsWith(":")) {
      continue;
    }
    if (line.startsWith("event:")) {
      eventType = line.slice(6).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).trim());
    }
  }

  const dataText = dataLines.join("\n");
  let data = {};
  if (dataText) {
    try {
      data = JSON.parse(dataText);
    } catch (error) {
      console.error("Failed to parse SSE payload", error, dataText);
      return;
    }
  }

  switch (eventType) {
    case "chunk":
      if (!currentAssistant) {
        currentAssistant = appendAssistantMessage();
      }
      currentAssistant.body.textContent += data.content ?? "";
      scrollToBottom();
      break;
    case "final":
      if (!currentAssistant) {
        currentAssistant = appendAssistantMessage();
      }
      currentAssistant.body.textContent = data.answer ?? currentAssistant.body.textContent;
      renderSources(currentAssistant.container, data.sources ?? []);
      conversationHistory = data.history ?? [];
      currentAssistant = null;
      scrollToBottom();
      break;
    case "error":
      showError(new Error(data.error || "Stream error"));
      break;
    case "done":
      break;
    default:
      console.debug("Unhandled SSE event", eventType, data);
  }
}

function appendUserMessage(text) {
  const container = document.createElement("article");
  container.className = "message message--user";

  const body = document.createElement("p");
  body.className = "message__body";
  body.textContent = text;
  container.appendChild(body);

  messagesEl.appendChild(container);
  scrollToBottom();
}

function appendAssistantMessage() {
  const container = document.createElement("article");
  container.className = "message message--agent";

  const body = document.createElement("p");
  body.className = "message__body";
  body.textContent = "";
  container.appendChild(body);

  messagesEl.appendChild(container);
  scrollToBottom();

  return { container, body };
}

function renderSources(container, sources) {
  let sourcesEl = container.querySelector(".sources");
  if (!sources || sources.length === 0) {
    if (sourcesEl) {
      sourcesEl.remove();
    }
    return;
  }

  if (!sourcesEl) {
    sourcesEl = document.createElement("div");
    sourcesEl.className = "sources";
    container.appendChild(sourcesEl);
  }

  sourcesEl.innerHTML = "";
  sources.forEach((source, index) => {
    const link = document.createElement("a");
    link.className = "sources__item";
    link.href = source.path || "#";
    link.target = "_blank";
    link.rel = "noreferrer";
    link.textContent = `${index + 1}. ${source.title}`;
    sourcesEl.appendChild(link);
  });
}

function setIngestState(active, message) {
  ingesting = active;
  if (fileInput) {
    fileInput.disabled = active;
  }
  if (ingestBtn) {
    ingestBtn.disabled = active;
    ingestBtn.textContent = active ? "Ingesting…" : "Ingest";
  }
  if (message !== undefined) {
    updateIngestStatus(message, false);
  }
}

function updateIngestStatus(message, isError = false) {
  if (!ingestStatusEl) {
    return;
  }
  ingestStatusEl.textContent = message;
  ingestStatusEl.classList.toggle("upload__status--error", Boolean(isError && message));
}

function showUploadError(error) {
  console.error(error);
  setIngestState(false);
  const message = error?.message || String(error);
  updateIngestStatus(`Error: ${message}`, true);
}

function showError(error) {
  console.error(error);
  if (!currentAssistant) {
    currentAssistant = appendAssistantMessage();
  }
  currentAssistant.body.textContent = `Error: ${error.message || error}`;
  currentAssistant = null;
  scrollToBottom();
}

function setStreamingState(active) {
  streaming = active;
  textareaEl.disabled = active;
  sendBtn.disabled = active;
  resetBtn.disabled = active;
  if (!active) {
    textareaEl.focus();
  }
}

function scrollToBottom() {
  messagesEl.scrollTop = messagesEl.scrollHeight;
}

textareaEl.focus();
