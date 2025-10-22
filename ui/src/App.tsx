import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';

type Role = 'user' | 'assistant';

type Source = {
  title: string;
  path?: string;
};

type ChatMessage = {
  id: string;
  role: Role;
  content: string;
  sources?: Source[];
  error?: boolean;
};

type HistoryTurn = {
  role: string;
  content: string;
};

type UploadStatus = 'pending' | 'uploading' | 'success' | 'error';

type UploadEntry = {
  id: string;
  name: string;
  size: number;
  status: UploadStatus;
  message?: string;
  chunks?: number;
  file: File;
};

type ToastTone = 'default' | 'success' | 'error';

type Toast = {
  id: string;
  message: string;
  tone: ToastTone;
};

const MAX_CHAT_LENGTH = 8000;

function createId() {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return Math.random().toString(36).slice(2);
}

function formatBytes(bytes: number) {
  if (bytes === 0) {
    return '0 B';
  }
  const units = ['B', 'KB', 'MB', 'GB'];
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, exponent);
  return `${value.toFixed(value >= 10 || exponent === 0 ? 0 : 1)} ${units[exponent]}`;
}

function isSupportedFile(name: string) {
  const lower = name.toLowerCase();
  return (
    lower.endsWith('.md') ||
    lower.endsWith('.markdown') ||
    lower.endsWith('.pdf') ||
    lower.endsWith('.csv')
  );
}

const dropZoneText = {
  idle: 'Add documents',
  dragging: 'Release to add files to the processing queue',
};

const statusLabels: Record<UploadStatus, string> = {
  pending: 'Waiting to process',
  uploading: 'Uploading…',
  success: 'Processed',
  error: 'Failed',
};

const toastDuration = 4200;

const App: React.FC = () => {
  const [question, setQuestion] = useState('');
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [history, setHistory] = useState<HistoryTurn[]>([]);
  const [streaming, setStreaming] = useState(false);
  const [uploads, setUploads] = useState<UploadEntry[]>([]);
  const [activeUploadId, setActiveUploadId] = useState<string | null>(null);
  const [isDragging, setIsDragging] = useState(false);
  const [toasts, setToasts] = useState<Toast[]>([]);
  const [sidebarOpen, setSidebarOpen] = useState(false);

  const messageListRef = useRef<HTMLDivElement | null>(null);
  const fileInputRef = useRef<HTMLInputElement | null>(null);
  const uploadAbortController = useRef<AbortController | null>(null);

  const scrollToBottom = useCallback(() => {
    const container = messageListRef.current;
    if (!container) {
      return;
    }
    requestAnimationFrame(() => {
      container.scrollTop = container.scrollHeight;
    });
  }, []);

  const pushToast = useCallback((message: string, tone: ToastTone = 'default') => {
    const id = createId();
    setToasts(prev => [...prev, { id, message, tone }]);
    window.setTimeout(() => {
      setToasts(prev => prev.filter(toast => toast.id !== id));
    }, toastDuration);
  }, []);

  const appendMessage = useCallback(
    (message: ChatMessage) => {
      setMessages(prev => [...prev, message]);
      requestAnimationFrame(scrollToBottom);
    },
    [scrollToBottom]
  );

  const updateMessage = useCallback(
    (id: string, updater: (message: ChatMessage) => ChatMessage) => {
      setMessages(prev => prev.map(message => (message.id === id ? updater(message) : message)));
    },
    []
  );

  const resetConversation = useCallback(() => {
    setMessages([]);
    setHistory([]);
    setQuestion('');
    setStreaming(false);
  }, []);

  const handleChatError = useCallback(
    (assistantId: string, error: unknown) => {
      console.error(error);
      updateMessage(assistantId, message => ({
        ...message,
        error: true,
        content:
          error instanceof Error
            ? `Error: ${error.message}`
            : typeof error === 'string'
              ? `Error: ${error}`
              : 'Error: Failed to fetch response',
      }));
      pushToast('Something went wrong while streaming the response.', 'error');
    },
    [pushToast, updateMessage]
  );

  const streamChat = useCallback(
    async (prompt: string, assistantId: string) => {
      const payload = {
        question: prompt,
        history,
      };

      const response = await fetch('/v1/chat/stream', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!response.ok || !response.body) {
        const message = await response.text();
        throw new Error(message || response.statusText);
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) {
          break;
        }
        buffer += decoder.decode(value, { stream: true });
        let boundary = buffer.indexOf('\n\n');
        while (boundary !== -1) {
          const rawEvent = buffer.slice(0, boundary);
          buffer = buffer.slice(boundary + 2);
          if (rawEvent.trim()) {
            const lines = rawEvent.split('\n');
            let eventType = 'message';
            const dataLines: string[] = [];
            for (const line of lines) {
              if (line.startsWith(':')) {
                continue;
              }
              if (line.startsWith('event:')) {
                eventType = line.slice(6).trim();
              } else if (line.startsWith('data:')) {
                dataLines.push(line.slice(5).trim());
              }
            }
            if (dataLines.length > 0) {
              try {
                const parsed = JSON.parse(dataLines.join('\n'));
                if (eventType === 'chunk') {
                  const chunkContent = typeof parsed.content === 'string' ? parsed.content : '';
                  if (chunkContent) {
                    updateMessage(assistantId, message => ({
                      ...message,
                      content: message.content + chunkContent,
                    }));
                  }
                } else if (eventType === 'final') {
                  const answer = typeof parsed.answer === 'string' ? parsed.answer : '';
                  const sources = Array.isArray(parsed.sources)
                    ? (parsed.sources as Source[]).filter(Boolean)
                    : undefined;
                  updateMessage(assistantId, message => ({
                    ...message,
                    content: answer || message.content,
                    sources,
                  }));
                  if (Array.isArray(parsed.history)) {
                    setHistory(parsed.history as HistoryTurn[]);
                  }
                } else if (eventType === 'error') {
                  const message = typeof parsed.error === 'string' ? parsed.error : 'Stream error';
                  throw new Error(message);
                }
              } catch (err) {
                console.error('Failed to parse SSE payload', err, dataLines);
              }
            }
          }
          boundary = buffer.indexOf('\n\n');
        }
      }
    },
    [history, updateMessage]
  );

  const submitQuestion = useCallback(
    async (event?: React.FormEvent<HTMLFormElement>) => {
      if (event) {
        event.preventDefault();
      }
      if (streaming) {
        return;
      }
      const trimmed = question.trim();
      if (!trimmed) {
        return;
      }
      if (trimmed.length > MAX_CHAT_LENGTH) {
        pushToast('Question is too long to send.', 'error');
        return;
      }

      const userMessage: ChatMessage = {
        id: createId(),
        role: 'user',
        content: trimmed,
      };
      const assistantMessage: ChatMessage = {
        id: createId(),
        role: 'assistant',
        content: '',
      };

      appendMessage(userMessage);
      appendMessage(assistantMessage);
      setStreaming(true);
      setQuestion('');

      try {
        await streamChat(trimmed, assistantMessage.id);
      } catch (error) {
        handleChatError(assistantMessage.id, error);
      } finally {
        setStreaming(false);
      }
    },
    [appendMessage, handleChatError, pushToast, question, streamChat, streaming]
  );

  const handleKeyDown = useCallback(
    (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        submitQuestion();
      }
    },
    [submitQuestion]
  );

  const handleFiles = useCallback(
    (fileList: FileList | File[]) => {
      const files = Array.from(fileList);
      if (!files.length) {
        return;
      }
      setUploads(previous => {
        const existingKeys = new Set(previous.map(entry => `${entry.name}:${entry.size}`));
        const nextEntries: UploadEntry[] = [];

        files.forEach(file => {
          if (!isSupportedFile(file.name)) {
            pushToast(`${file.name} is not a supported format.`, 'error');
            return;
          }
          const key = `${file.name}:${file.size}`;
          if (
            existingKeys.has(key) ||
            nextEntries.some(entry => `${entry.name}:${entry.size}` === key)
          ) {
            pushToast(`${file.name} is already queued.`, 'default');
            return;
          }

          nextEntries.push({
            id: createId(),
            name: file.name,
            size: file.size,
            status: 'pending',
            file,
          });
        });

        if (nextEntries.length === 0) {
          return previous;
        }
        pushToast(
          `${nextEntries.length} file${nextEntries.length > 1 ? 's' : ''} queued for processing.`,
          'success'
        );
        return [...previous, ...nextEntries];
      });

      if (fileInputRef.current) {
        fileInputRef.current.value = '';
      }
    },
    [pushToast]
  );

  useEffect(() => {
    if (activeUploadId) {
      return;
    }
    const pending = uploads.find(entry => entry.status === 'pending');
    if (!pending) {
      return;
    }

    const controller = new AbortController();
    uploadAbortController.current = controller;
    const { file, id, name } = pending;

    const ingestFile = async () => {
      setActiveUploadId(id);
      setUploads(previous =>
        previous.map(entry => (entry.id === id ? { ...entry, status: 'uploading' } : entry))
      );

      try {
        const formData = new FormData();
        formData.append('document', file);

        const response = await fetch('/v1/ingest/upload', {
          method: 'POST',
          body: formData,
          signal: controller.signal,
        });

        const payload = await response.json().catch(() => ({}));

        if (!response.ok) {
          const message = payload?.error || `Upload failed (${response.status})`;
          throw new Error(message);
        }

        const chunks: number | undefined =
          typeof payload?.document?.chunks === 'number' ? payload.document.chunks : undefined;
        const successMessage =
          typeof payload?.message === 'string' && payload.message.length > 0
            ? payload.message
            : `Processed ${name}`;

        setUploads(previous =>
          previous.map(entry =>
            entry.id === id
              ? {
                  ...entry,
                  status: 'success',
                  message: successMessage,
                  chunks,
                }
              : entry
          )
        );
        pushToast(`${name} processed successfully`, 'success');
      } catch (error) {
        console.error(error);
        const message = error instanceof Error ? error.message : 'Upload failed';
        setUploads(previous =>
          previous.map(entry =>
            entry.id === id
              ? {
                  ...entry,
                  status: 'error',
                  message,
                }
              : entry
          )
        );
        pushToast(`${name} failed: ${message}`, 'error');
      } finally {
        setActiveUploadId(null);
        uploadAbortController.current = null;
      }
    };

    void ingestFile();
  }, [activeUploadId, pushToast, uploads]);

  useEffect(() => () => uploadAbortController.current?.abort(), []);

  const uploadSummary = useMemo(() => {
    if (!uploads.length) {
      return 'Uploads appear here as they are processed.';
    }
    const completed = uploads.filter(entry => entry.status === 'success').length;
    const pending = uploads.filter(
      entry => entry.status === 'pending' || entry.status === 'uploading'
    ).length;
    if (pending === 0) {
      return `All uploads processed (${completed} complete).`;
    }
    return `${pending} file${pending === 1 ? '' : 's'} in queue, ${completed} complete.`;
  }, [uploads]);

  const renderSources = useCallback((sources?: Source[]) => {
    if (!sources || sources.length === 0) {
      return null;
    }
    return (
      <div className="source-list">
        {sources.map((source, index) => (
          <span key={`${source.path ?? source.title}-${index}`} className="source-item" role="text">
            <span aria-hidden>📄</span>
            {source.title || source.path || `Source ${index + 1}`}
          </span>
        ))}
      </div>
    );
  }, []);

  const handleDragOver = useCallback((event: React.DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.stopPropagation();
    setIsDragging(true);
    event.dataTransfer.dropEffect = 'copy';
  }, []);

  const handleDragLeave = useCallback((event: React.DragEvent<HTMLDivElement>) => {
    event.preventDefault();
    event.stopPropagation();
    setIsDragging(false);
  }, []);

  const handleDrop = useCallback(
    (event: React.DragEvent<HTMLDivElement>) => {
      event.preventDefault();
      event.stopPropagation();
      setIsDragging(false);
      if (event.dataTransfer?.files) {
        handleFiles(event.dataTransfer.files);
      }
    },
    [handleFiles]
  );

  return (
    <div className="app-shell">
      <div className="app-container">
        <header className="app-header">
          <div>
            <h1 className="app-title">Memory</h1>
            <p className="app-subtitle">
              Chat with your Markdown, PDF, and CSV sources. Upload new knowledge directly into your
              retrieval index.
            </p>
          </div>
          <div className="session-actions">
            <button
              className="secondary-button"
              type="button"
              onClick={resetConversation}
              disabled={streaming || messages.length === 0}
            >
              Start new session
            </button>
          </div>
        </header>

        <div className={`chat-panel${sidebarOpen ? ' sidebar-open' : ''}`}>
          <section className="message-board">
            <div
              className="message-list"
              ref={messageListRef}
              onDragOver={handleDragOver}
              onDragEnter={handleDragOver}
              onDragLeave={handleDragLeave}
              onDrop={handleDrop}
            >
              {messages.length === 0 ? (
                <div className="empty-state">
                  <strong>Upload a document or ask a question to get started.</strong>
                  <span>Drag files anywhere in this panel or use the uploader on the right.</span>
                </div>
              ) : (
                messages.map(message => (
                  <article
                    key={message.id}
                    className={`message ${message.role}${message.error ? ' error' : ''}`}
                  >
                    <p>{message.content || (message.role === 'assistant' ? 'Thinking…' : '')}</p>
                    <div className="message-footer">
                      <span>{message.role === 'user' ? 'You' : 'Assistant'}</span>
                      {renderSources(message.sources)}
                    </div>
                  </article>
                ))
              )}
            </div>

            <form className="composer" onSubmit={submitQuestion}>
              <label className="visually-hidden" htmlFor="composer-input">
                Ask a question
              </label>
              <textarea
                id="composer-input"
                name="question"
                value={question}
                disabled={streaming}
                placeholder="Ask the assistant anything about your documents…"
                onChange={event => setQuestion(event.target.value)}
                onKeyDown={handleKeyDown}
                rows={3}
              />
              <button type="submit" disabled={streaming || question.trim().length === 0}>
                {streaming ? 'Streaming…' : 'Send'}
              </button>
            </form>
          </section>

          <aside className={`sidebar${sidebarOpen ? ' expanded' : ' collapsed'}`}>
            <button
              type="button"
              className={`sidebar-tab${sidebarOpen ? ' active' : ''}`}
              aria-expanded={sidebarOpen}
              onClick={() => setSidebarOpen(open => !open)}
            >
              <span aria-hidden>{sidebarOpen ? '⟨' : '⟩'}</span>
              <span className="sidebar-tab__label visually-hidden">Add</span>
            </button>

            <div className="sidebar-content" aria-hidden={!sidebarOpen}>
              <section className="upload-card">
                <h2 className="upload-title">Add knowledge</h2>
                <p className="app-subtitle">
                  Queue one or more files to add knowledge to my brain.
                </p>

                <div
                  className={`upload-dropzone${isDragging ? ' dragging' : ''}`}
                  onClick={() => fileInputRef.current?.click()}
                  onKeyDown={event => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      fileInputRef.current?.click();
                    }
                  }}
                  role="button"
                  tabIndex={0}
                  onDragOver={handleDragOver}
                  onDragEnter={handleDragOver}
                  onDragLeave={handleDragLeave}
                  onDrop={handleDrop}
                >
                  <strong>{isDragging ? dropZoneText.dragging : dropZoneText.idle}</strong>
                  <span>Markdown (.md), PDF (.pdf) and CSV (.csv) supported</span>
                  <div className="upload-actions">
                    <button
                      type="button"
                      className="upload-button"
                      onClick={() => fileInputRef.current?.click()}
                    >
                      Browse files
                    </button>
                    <input
                      ref={fileInputRef}
                      id="document-input"
                      className="visually-hidden"
                      type="file"
                      name="documents"
                      multiple
                      accept=".md,.markdown,.pdf,.csv"
                      onChange={event => {
                        if (event.target.files) {
                          handleFiles(event.target.files);
                        }
                      }}
                    />
                  </div>
                </div>

                <p className="app-subtitle">{uploadSummary}</p>

                <div className="upload-list" role="list">
                  {uploads.map(entry => (
                    <article key={entry.id} className="upload-item" role="listitem">
                      <header>
                        <span>{entry.name}</span>
                        <span>{formatBytes(entry.size)}</span>
                      </header>
                      <div className="upload-status" data-status={entry.status}>
                        <span>
                          {statusLabels[entry.status]}
                          {entry.status === 'success' && typeof entry.chunks === 'number'
                            ? ` • ${entry.chunks} chunk${entry.chunks === 1 ? '' : 's'}`
                            : null}
                        </span>
                        {entry.message && <span>— {entry.message}</span>}
                      </div>
                    </article>
                  ))}
                </div>
              </section>
            </div>
          </aside>
        </div>

        <div className="toast-container" aria-live="polite" aria-atomic="true">
          {toasts.map(toast => (
            <div key={toast.id} className={`toast${toast.tone === 'error' ? ' error' : ''}`}>
              {toast.message}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

export default App;
