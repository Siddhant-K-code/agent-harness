"use strict";
let chatID = sessionStorage.getItem("harness-chat") || null;
let chatData = null,
  chatSignature = "",
  chatDraft = "",
  chatSending = false;
let chatRequest = null,
  chatPreset = 0;
const requestID = () => crypto.randomUUID().replaceAll("-", "");

function chatError(message) {
  const n = $("#chat-error");
  if (n) n.textContent = message;
}
function field(label, input) {
  const l = el("label", "", label);
  l.append(input);
  return l;
}
function chatNumber(id, value, min, max, step = "1") {
  const n = el("input");
  Object.assign(n, {
    id,
    type: "number",
    min: String(min),
    max: String(max),
    step,
    value: String(value),
    required: true,
  });
  return n;
}
function renderChat(root) {
  if (!$("#chat-shell")) {
    root.replaceChildren(
      heading(
        "Project chat",
        "Understand the code. Work out a change. Give the agent a task.",
      ),
    );
    const shell = el("div", "chat-layout");
    shell.id = "chat-shell";
    const sidebar = el("section", "panel chat-sidebar");
    const project = el("select");
    project.id = "chat-project";
    (data.tasks || []).forEach((t, i) => {
      const o = el("option", "", t.name);
      o.value = i;
      project.append(o);
    });
    project.value = String(chatPreset);
    project.addEventListener("change", () => {
      chatPreset = Number(project.value);
      if (!chatData) {
        fillChatSettings();
        updateChatControls();
      }
    });
    const create = button("+ New conversation", "quiet", async () => {
      try {
        await createConversation();
      } catch (e) {
        chatError(e.message);
      }
    });
    create.disabled = !data.tasks?.length;
    sidebar.append(
      field("Project for new chat", project),
      create,
      el("p", "eyebrow section-gap", "CONVERSATIONS"),
    );
    const list = el("div", "chat-list");
    list.id = "chat-list";
    sidebar.append(list);
    const main = el("section", "panel chat-main");
    const title = el("div", "chat-context");
    title.id = "chat-context";
    const messages = el("div", "chat-messages");
    messages.id = "chat-messages";
    messages.setAttribute("aria-label", "Conversation messages");
    const form = el("form", "chat-composer");
    form.id = "chat-form";
    const error = el("p", "error");
    error.id = "chat-error";
    error.setAttribute("role", "alert");
    const textarea = el("textarea");
    Object.assign(textarea, {
      id: "chat-message",
      rows: 3,
      maxLength: 32000,
      placeholder: "Ask about this project, or describe a task…",
      value: chatDraft,
    });
    textarea.setAttribute("aria-label", "Message");
    textarea.addEventListener("input", () => {
      chatDraft = textarea.value;
    });
    textarea.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        if (!$("#chat-send").disabled) form.requestSubmit();
      }
    });
    const settings = el("details", "chat-settings"),
      sum = el("summary", "", "Model & budget");
    settings.append(sum);
    const grid = el("div", "form-grid");
    const model = el("select");
    model.id = "chat-model";
    for (const m of data.models || []) {
      const o = el("option", "", m.id);
      model.append(o);
    }
    grid.append(
      field("Model", model),
      field(
        "Maximum per question ($)",
        chatNumber("chat-usd", 0.1, 0.01, 100, "0.01"),
      ),
      field(
        "Context window (tokens)",
        chatNumber("chat-window", 32768, 1024, 1050000),
      ),
      field(
        "Reserved output (tokens)",
        chatNumber("chat-output", 1024, 256, 128000),
      ),
    );
    settings.append(
      grid,
      el(
        "p",
        "footnote",
        "Each question uses your API key and reserves its own budget. Up to eight responses, including repository reads. Recent conversation is included within the context limit.",
      ),
    );
    const actions = el("div", "chat-actions");
    const stop = button("Stop", "quiet danger", async () => {
      try {
        await api("chats/" + chatID + "/cancel", {});
        chatError("Stopping the active question or run…");
      } catch (e) {
        chatError(e.message);
      }
    });
    stop.id = "chat-stop";
    const run = button("Run task…", "quiet", () =>
      taskFromChat($("#chat-message").value),
    );
    run.id = "chat-run";
    const send = el("button", "primary", "Ask project");
    send.id = "chat-send";
    send.type = "submit";
    actions.append(stop, run, send);
    form.append(
      error,
      textarea,
      settings,
      actions,
      el(
        "p",
        "footnote",
        "Ask reads committed files. Run task opens the coding setup for review. Enter to ask · Shift+Enter for a new line.",
      ),
    );
    form.addEventListener("submit", sendQuestion);
    main.append(title, messages, form);
    shell.append(sidebar, main);
    root.append(shell);
    fillChatSettings();
    renderMessages();
  }
  const list = $("#chat-list");
  list.replaceChildren();
  for (const c of data.chats || []) {
    const b = button(
      "",
      "chat-row" + (c.id === chatID ? " selected" : ""),
      () => selectChat(c.id),
    );
    b.append(
      el("strong", "", c.title),
      el("span", "meta", c.turns + " messages · " + short(c.base)),
    );
    if (c.state === "running") b.append(el("small", "pending", "Working…"));
    list.append(b);
  }
  if (!data.chats?.length)
    list.append(el("p", "muted", "Your conversations will appear here."));
  updateChatControls();
}
function fillChatSettings() {
  const t = chatData?.conversation.task || data.tasks?.[chatPreset];
  if (!t || !$("#chat-model")) return;
  $("#chat-model").value = t.model;
  $("#chat-window").value = Math.min(
    32768,
    t.limits.context_window_tokens || 200000,
  );
  $("#chat-output").value = Math.min(2048, t.limits.max_output_tokens);
  $("#chat-usd").value = Math.min(0.1, t.limits.max_usd);
  $("#chat-usd").max = t.limits.max_usd;
}
function updateChatControls() {
  if (!$("#chat-send")) return;
  const allowed = chatData ? chatData.available : !!data.tasks?.length;
  const active = chatData?.conversation.turns.some(
    (t) => t.state === "running",
  );
  $("#chat-send").disabled =
    chatSending ||
    data.busy ||
    (chatID && !chatData) ||
    !allowed ||
    !data.key_present;
  $("#chat-run").disabled =
    chatSending ||
    data.busy ||
    (chatID && !chatData) ||
    !allowed ||
    !data.key_present;
  $("#chat-stop").hidden = !active;
  const title = $("#chat-context");
  title.replaceChildren();
  const t = chatData?.conversation.task || data.tasks?.[chatPreset];
  title.append(el("strong", "", t?.name || "Choose a prepared project task"));
  title.append(
    el(
      "span",
      "meta",
      t
        ? t.repository.split(/[\\/]/).pop() +
            " · " +
            (chatData
              ? "commit " + short(t.ref)
              : "commit pinned when the conversation starts")
        : "Start harness serve with --task harness.task.json to enable project chat.",
    ),
  );
  if (chatData && !allowed)
    title.append(
      el(
        "p",
        "error",
        "This prepared task changed or is no longer configured. History remains available; start a new conversation to continue.",
      ),
    );
}
async function createConversation() {
  if (!data.tasks?.length)
    throw new Error(
      "Configure a project task with harness serve --task harness.task.json.",
    );
  const c = await api("chats", { task: Number($("#chat-project").value) });
  await selectChat(c.id);
  await refresh();
}
async function selectChat(id) {
  chatID = id;
  sessionStorage.setItem("harness-chat", id);
  chatData = null;
  chatSignature = "";
  chatRequest = null;
  chatDraft = "";
  if ($("#chat-message")) $("#chat-message").value = "";
  chatError("");
  renderMessages();
  await refreshChat();
  fillChatSettings();
  renderChat($("#view"));
}
async function refreshChat() {
  if (!chatID || view !== "chat") return;
  const id = chatID;
  try {
    const d = await api("chats/" + id);
    if (chatID !== id) return;
    const first = !chatData;
    chatData = d;
    const sig = JSON.stringify(d);
    if (sig !== chatSignature) {
      chatSignature = sig;
      renderMessages();
    }
    if (first) {
      if (d.task_index >= 0) {
        chatPreset = d.task_index;
        $("#chat-project").value = chatPreset;
      }
      fillChatSettings();
    }
    updateChatControls();
  } catch (e) {
    chatError(e.message);
  }
}
// Markdown's useful subset rendered exclusively through text nodes. No HTML,
// remote images, executable links, or untrusted DOM insertion.
function chatText(text) {
  const box = el("div", "chat-prose");
  let code = null;
  for (const line of text.split("\n")) {
    if (line.startsWith("```")) {
      if (code) {
        box.append(code);
        code = null;
      } else code = el("pre", "code");
      continue;
    }
    if (code) {
      code.append(document.createTextNode(line + "\n"));
      continue;
    }
    const h = /^#{1,4}\s+(.+)$/.exec(line);
    const p = el(h ? "h3" : "p", "", undefined);
    const content = h ? h[1] : line;
    const parts = content.split(/(`[^`]+`|\*\*[^*]+\*\*)/g);
    for (const part of parts) {
      if (part.startsWith("`") && part.endsWith("`"))
        p.append(el("code", "", part.slice(1, -1)));
      else if (part.startsWith("**") && part.endsWith("**"))
        p.append(el("strong", "", part.slice(2, -2)));
      else p.append(document.createTextNode(part));
    }
    box.append(p);
  }
  if (code) box.append(code);
  return box;
}
function renderMessages() {
  const box = $("#chat-messages");
  if (!box) return;
  const atBottom = box.scrollHeight - box.scrollTop - box.clientHeight < 100;
  const scroll = box.scrollTop;
  const open = new Set(
    [...box.querySelectorAll("details[open]")].map((d) => d.dataset.source),
  );
  box.replaceChildren();
  if (chatID && !chatData) {
    box.append(el("p", "empty", "Loading conversation…"));
    return;
  }
  const turns = chatData?.conversation.turns || [];
  if (!turns.length) {
    const welcome = el("div", "chat-welcome");
    welcome.append(
      el("span", "eyebrow", "START WITH THE PROJECT"),
      el("h2", "", "What are we working on?"),
      el(
        "p",
        "sub",
        "Ask a question, talk through a change, or hand the agent a concrete task.",
      ),
    );
    const suggestions = el("div", "chat-suggestions");
    for (const text of [
      "Explain how this project is structured.",
      "What should I change next, and how would I verify it?",
      "Find the code responsible for the main behavior.",
    ]) {
      suggestions.append(
        button(text, "quiet", () => {
          $("#chat-message").value = text;
          chatDraft = text;
          $("#chat-message").focus();
        }),
      );
    }
    welcome.append(suggestions);
    box.append(welcome);
  }
  for (const t of turns) {
    const user = el("article", "chat-message user-message");
    user.append(
      el(
        "div",
        "eyebrow",
        "YOU · " + (t.kind === "ask" ? "QUESTION" : "CODING TASK"),
      ),
      el("p", "message-text", t.question),
    );
    box.append(user);
    const reply = el("article", "chat-message assistant-message");
    reply.append(el("div", "eyebrow", "HARNESS"));
    if (t.answer) reply.append(chatText(t.answer));
    if (t.state === "running") {
      const live = el("p", "pending", t.phase + "…");
      live.setAttribute("role", "status");
      reply.append(live);
    }
    if (t.error) reply.append(el("p", "error", t.error));
    if (t.sources?.length) {
      const sources = el("details", "chat-sources");
      sources.dataset.source = t.id;
      sources.open = open.has(t.id);
      sources.append(
        el(
          "summary",
          "",
          t.sources.length + " repository reads · inspect sources",
        ),
      );
      t.sources.forEach((s) => {
        sources.append(
          el("p", "meta", s.tool + " · " + s.path),
          el("pre", "code", s.text),
        );
      });
      reply.append(sources);
    }
    if (t.run_id) {
      const recorded = data.runs.find((x) => x.run.id === t.run_id),
        status = recorded?.run.state || t.state;
      const card = el("div", "chat-run-card");
      card.append(
        badge(status),
        el("code", "", short(t.run_id)),
        button("Open run & patch →", "quiet", () => {
          selected = t.run_id;
          detail = null;
          signature = "";
          view = "runs";
          render();
          refreshDetail();
        }),
      );
      reply.append(card);
    }
    const footer = el("div", "chat-message-footer");
    footer.append(
      el(
        "span",
        "meta",
        t.settings.model +
          " · " +
          money(t.estimated_usd_uncached) +
          " recorded" +
          (t.billing_unknown
            ? t.pending && t.state === "running"
              ? " · request in flight"
              : " · additional billing unknown"
            : "") +
          " · " +
          t.state,
      ),
    );
    if (t.omitted_turns)
      footer.append(
        el(
          "span",
          "meta",
          t.omitted_turns + " older questions omitted from model context",
        ),
      );
    if (t.kind === "ask" && t.state === "completed")
      footer.append(
        button("Use answer as a task…", "quiet", () =>
          taskFromChat(
            "Implement the proposed change while preserving the prepared task requirements. Verify the result.\n\nDiscussion:\n" +
              t.question +
              "\n\nProposed approach from our conversation (verify against the code):\n" +
              t.answer,
          ),
        ),
      );
    reply.append(footer);
    box.append(reply);
  }
  if (atBottom || chatSending) box.scrollTop = box.scrollHeight;
  else box.scrollTop = scroll;
}
async function sendQuestion(e) {
  e.preventDefault();
  if (chatSending) return;
  const message = $("#chat-message").value.trim();
  if (!message) {
    chatError("Write a question first.");
    return;
  }
  chatSending = true;
  chatError("");
  updateChatControls();
  try {
    // Capture edited settings before creating a conversation resets defaults.
    const payload = {
      message,
      model: $("#chat-model").value,
      context_window_tokens: Number($("#chat-window").value),
      max_output_tokens: Number($("#chat-output").value),
      max_usd: Number($("#chat-usd").value),
    };
    if (!chatID) await createConversation();
    const hash = JSON.stringify({ chatID, ...payload });
    if (!chatRequest || chatRequest.hash !== hash)
      chatRequest = { hash, id: requestID() };
    await api("chats/" + chatID + "/ask", {
      request_id: chatRequest.id,
      ...payload,
    });
    chatDraft = "";
    $("#chat-message").value = "";
    chatRequest = null;
    await refresh();
    await refreshChat();
  } catch (e) {
    chatError(
      e.message +
        " Check the conversation before retrying; an accepted question keeps the same request ID.",
    );
  } finally {
    chatSending = false;
    updateChatControls();
  }
}
async function taskFromChat(message) {
  if (!message.trim()) {
    chatError(
      "Describe the task first, or use an answer as the starting point.",
    );
    return;
  }
  try {
    if (!chatID) await createConversation();
    if (!chatData?.available)
      throw new Error("Start a new conversation with a configured task.");
    openRun();
    runChat = chatID;
    runRequestID = requestID();
    $("#preset").value = chatData.task_index;
    $("#preset").disabled = true;
    fillPreset();
    $("#goal").value = message;
    $("#preset-info").textContent =
      chatData.conversation.task.repository.split(/[\\/]/).pop() +
      " · pinned commit " +
      short(chatData.conversation.task.ref);
  } catch (e) {
    chatError(e.message);
  }
}
