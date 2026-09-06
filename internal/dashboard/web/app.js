"use strict";
const $ = (s) => document.querySelector(s);
const el = (tag, cls, text) => {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
};
const fragment = new URLSearchParams(location.hash.slice(1));
if (fragment.has("token")) {
  sessionStorage.setItem("harness-token", fragment.get("token"));
  history.replaceState(null, "", location.pathname);
}
const token = sessionStorage.getItem("harness-token") || "";
window.addEventListener("hashchange", () => {
  if (new URLSearchParams(location.hash.slice(1)).has("token"))
    location.reload();
});
let data = null,
  view = "chat",
  selected = null,
  detail = null,
  detailTab = "patch",
  signature = "",
  loading = false,
  followNewRun = null;
const money = (n) => "$" + Number(n || 0).toFixed(4);
const short = (s) => (s || "").slice(0, 12);
const badge = (state) =>
  el("span", "badge " + state, state.replaceAll("_", " "));
async function api(path, body) {
  const r = await fetch("/api/" + path, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      Authorization: "Bearer " + token,
      ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
    },
    ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
  });
  if (!r.ok) throw new Error(await r.text());
  return r.json();
}
function notice(text) {
  $("#notice").replaceChildren(...(text ? [el("div", "banner", text)] : []));
}
function heading(title, sub, action) {
  const h = el("div", "heading"),
    t = el("div");
  t.append(el("h1", "", title), el("p", "sub", sub));
  h.append(t);
  if (action) h.append(action);
  return h;
}
function button(text, cls, fn) {
  const b = el("button", cls, text);
  b.type = "button";
  b.addEventListener("click", fn);
  return b;
}
function metric(label, value) {
  const m = el("div", "metric");
  m.append(el("small", "", label), el("strong", "", value));
  return m;
}
function render() {
  if (!data) return;
  const root = $("#view");
  if (view !== "chat" && view !== "projects") root.replaceChildren();
  document
    .querySelectorAll("[data-view]")
    .forEach((b) => b.classList.toggle("active", b.dataset.view === view));
  if (view === "chat") renderChat(root);
  if (view === "projects") renderProjects(root);
  if (view === "runs") renderRuns(root);
  if (view === "skills") renderSkills(root);
  if (view === "connections") renderConnections(root);
  if (view === "prompt") renderPrompt(root);
}
function renderRuns(root) {
  const launch = button("+ New run", "primary", openRun);
  launch.disabled = !data.tasks?.length || data.busy || !data.key_present;
  root.append(
    heading(
      "Runs",
      "From a task to a patch, with evidence at every step.",
      launch,
    ),
  );
  const runs = data.runs || [],
    reports = runs.map((x) => x.report).filter(Boolean),
    metrics = el("div", "metrics");
  metrics.append(
    metric("Recorded runs", runs.length),
    metric(
      "Verified completions",
      reports.filter((r) => r.verified && r.state === "completed").length,
    ),
    metric(
      "Recorded run spend",
      money(reports.reduce((s, r) => s + r.estimated_usd_uncached, 0)),
    ),
    metric(
      "Active runs",
      runs.filter((x) =>
        ["queued", "running", "cancelling"].includes(x.run.state),
      ).length,
    ),
  );
  root.append(metrics);
  if (data.busy)
    root.append(
      el("p", "pending", "Run in progress. This view refreshes automatically."),
    );
  if (data.last_error) root.append(el("p", "error", data.last_error));
  if (!runs.length) {
    root.append(
      el(
        "div",
        "panel empty",
        data.tasks?.length
          ? "No runs yet. Start the prepared task to create your first verified patch."
          : "No runs in this state directory. To enable launching tasks, start harness serve --task harness.task.json.",
      ),
    );
    return;
  }
  if (!selected || !runs.some((x) => x.run.id === selected))
    selected = runs[0].run.id;
  const layout = el("div", "run-layout"),
    left = el("section", "panel");
  left.append(el("div", "panel-title", "Recent runs · newest first"));
  const list = el("div", "run-list");
  for (const { run, report } of runs) {
    const b = button(
      "",
      "run-row" + (selected === run.id ? " selected" : ""),
      () => {
        selected = run.id;
        signature = "";
        render();
        refreshDetail();
      },
    );
    b.append(el("span", "row-title", run.name));
    const bottom = el("div", "row-bottom");
    bottom.append(
      badge(run.state),
      el(
        "span",
        "meta",
        report ? money(report.estimated_usd_uncached) : run.steps + " steps",
      ),
    );
    b.append(bottom);
    list.append(b);
  }
  left.append(list);
  const right = el("section", "panel");
  right.id = "run-detail";
  layout.append(left, right);
  root.append(
    layout,
    el(
      "p",
      "footnote",
      "Showing up to 200 runs. Run spend is estimated from reported usage. Skill proposals, in-flight usage, and infrastructure charges are separate.",
    ),
  );
  renderDetail();
}
async function refreshDetail() {
  if (!selected || view !== "runs") return;
  const id = selected;
  try {
    const d = await api("runs/" + id);
    if (id !== selected) return;
    const sig = JSON.stringify(d);
    if (sig !== signature) {
      signature = sig;
      detail = d;
      renderDetail();
    }
  } catch (e) {
    notice(e.message);
  }
}
function renderDetail() {
  const box = $("#run-detail");
  if (!box) return;
  const openEvents = new Set(
    [...box.querySelectorAll("details[open]")].map((d) => d.dataset.sequence),
  );
  box.replaceChildren();
  if (!detail || detail.run.id !== selected) {
    box.append(el("p", "empty", "Loading run evidence…"));
    return;
  }
  const r = detail.run,
    p = detail.report || {},
    head = el("div", "detail-header");
  head.append(
    badge(r.state),
    el("h2", "", r.name),
    el(
      "p",
      "meta",
      r.id + " · " + r.task.model + " · " + (r.backend || "docker"),
    ),
  );
  box.append(head);
  const body = el("div", "detail-body");
  body.append(el("p", "sub", r.task.goal));
  const metrics = el("div", "detail-grid");
  for (const [label, value] of [
    [
      "Model cost",
      p.billing_unknown
        ? "Unknown"
        : detail.report
          ? money(p.estimated_usd_uncached)
          : "Pending",
    ],
    [
      "Verifier",
      p.verified
        ? "Passed"
        : p.verification_attempts
          ? "Not passed"
          : "Pending",
    ],
    ["Compactions", String(p.compactions || 0)],
  ]) {
    const c = el("div");
    c.append(el("small", "", label), el("span", "", value));
    metrics.append(c);
  }
  body.append(metrics);
  if (
    r.state === "completed" &&
    p.verified &&
    p.cleanup_confirmed &&
    p.patch_sha256
  )
    body.append(
      button("Review & create draft PR", "primary section-gap", () =>
        openDelivery(r.id),
      ),
    );
  if (r.reason) body.append(el("p", "muted", r.reason));
  if (p.external_outcome_unknown)
    body.append(
      el(
        "p",
        "error",
        "An external call has an uncertain outcome. Inspect the service before retrying.",
      ),
    );
  if (["running", "queued", "cancelling"].includes(r.state))
    body.append(
      button("Cancel run", "quiet danger", async () => {
        try {
          await api("runs/" + r.id + "/cancel", {});
          await refresh();
        } catch (e) {
          notice(e.message);
        }
      }),
    );
  const tabs = el("div", "tabs");
  for (const name of ["patch", "events", "configuration", "prompt"])
    tabs.append(
      button(
        name[0].toUpperCase() + name.slice(1),
        " " + (detailTab === name ? "active" : ""),
        () => {
          detailTab = name;
          renderDetail();
        },
      ),
    );
  body.append(tabs);
  if (detailTab === "patch") {
    if (detail.patch) {
      const pre = el("pre", "code");
      for (const line of detail.patch.split("\n"))
        pre.append(
          el(
            "span",
            "diff-line " +
              (line.startsWith("+")
                ? "add"
                : line.startsWith("-")
                  ? "remove"
                  : line.startsWith("@@")
                    ? "hunk"
                    : ""),
            line || " ",
          ),
        );
      body.append(pre);
      body.append(
        button("Download patch", "quiet section-gap", () =>
          download(r.id + ".patch", detail.patch),
        ),
      );
    } else
      body.append(
        el(
          "p",
          "empty",
          detail.patch_error ||
            "The controller saves the patch when the run ends.",
        ),
      );
  } else if (detailTab === "events") {
    for (const event of detail.events) {
      const d = el("details", "event"),
        sum = el("summary");
      sum.append(
        el("span", "", event.sequence + " · " + event.type),
        el("span", "meta", new Date(event.created_at).toLocaleTimeString()),
      );
      d.dataset.sequence = String(event.sequence);
      d.open = openEvents.has(String(event.sequence));
      d.append(sum, el("pre", "code", JSON.stringify(event.data, null, 2)));
      body.append(d);
    }
  } else if (detailTab === "configuration") {
    body.append(
      el(
        "pre",
        "code",
        JSON.stringify({ task: r.task, report: detail.report }, null, 2),
      ),
    );
  } else {
    body.append(
      el(
        "pre",
        "code",
        detail.prompt
          ? detail.prompt.instructions
          : "This run predates versioned prompt artifacts.",
      ),
    );
    if (detail.prompt)
      body.append(
        el(
          "p",
          "footnote",
          detail.prompt.version + " · " + detail.prompt.sha256,
        ),
      );
  }
  box.append(body);
}
function download(name, text) {
  const a = document.createElement("a"),
    url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
  a.href = url;
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
function renderSkills(root) {
  root.append(
    heading(
      "Skills",
      "Repository-scoped guidance, pinned to a specific version for every run.",
    ),
  );
  const cards = el("div", "cards");
  for (const s of data.skills) {
    const c = el("article", "card");
    c.append(
      badge(s.source),
      el("h2", "", s.id),
      el("p", "", s.description),
      el("p", "meta", s.repository),
      el("code", "", short(s.version)),
      el(
        "p",
        "",
        s.previous +
          " prior versions · " +
          (s.expires_at
            ? "Expires " + new Date(s.expires_at).toLocaleDateString()
            : "No expiry"),
      ),
    );
    cards.append(c);
  }
  if (!data.skills.length)
    cards.append(
      el(
        "div",
        "panel empty",
        "No imported skills in this state directory. Import a SKILL.md to make it available for explicit selection.",
      ),
    );
  root.append(
    cards,
    el("h3", "section-gap", "Import, select, evaluate"),
    el(
      "pre",
      "code",
      "harness skills import --repo /path/to/repo --id coding --file SKILL.md\nharness config set --task harness.task.json --skill coding\nharness learn init learning-lab",
    ),
    el(
      "p",
      "footnote",
      "Learned revisions are activated only after paired evaluations pass. Promotion and rollback remain explicit CLI operations.",
    ),
  );
}
function renderConnections(root) {
  root.append(
    heading(
      "Connections",
      "Host-side access, selected per task. Credentials stay outside the sandbox.",
    ),
  );
  const cards = el("div", "cards");
  let c = el("article", "card");
  c.append(
    badge(data.key_present ? "completed" : "failed"),
    el("h2", "", "OpenAI · bring your own key"),
    el(
      "p",
      "",
      data.key_present
        ? "Configured through " + data.key_source
        : "No local API key configured.",
    ),
    el("pre", "code", "harness auth login\nharness auth status"),
  );
  cards.append(c);
  c = el("article", "card");
  c.append(
    el("h2", "", "GitHub"),
    el(
      "p",
      "",
      "Allowed repositories: " +
        ((data.integrations.github_repositories || []).join(", ") || "none"),
    ),
    el(
      "pre",
      "code",
      "gh auth login --hostname github.com --web\nharness github status\nharness github clone OWNER/REPO ./repo",
    ),
  );
  cards.append(c);
  for (const [name, s] of Object.entries(data.integrations.mcp || {})) {
    c = el("article", "card");
    c.append(
      badge("configured"),
      el("h2", "", name),
      el("p", "", s.url),
      el("p", "", "Enabled tools: " + s.tools.join(", ")),
      el(
        "p",
        "",
        "Authentication: " +
          (s.token_env ? "environment variable " + s.token_env : "no token"),
      ),
    );
    cards.append(c);
  }
  root.append(cards);
  if (data.integration_error)
    root.append(el("p", "error", data.integration_error));
  root.append(
    el("h3", "section-gap", "Connect an MCP server"),
    el(
      "pre",
      "code",
      "harness integrations add-mcp --name docs --url https://your-server.example/mcp --allow-tool search --token-env DOCS_MCP_TOKEN\nharness integrations select --mcp-tool docs/search\nharness integrations check",
    ),
    el(
      "p",
      "footnote",
      "MCP uses Streamable HTTP. The operator enables exact tools; tasks select a subset. Configuration is not a health check. Run integrations check to connect. Only enable tools whose effects and data access you intend to grant. Stdio launch and OAuth setup are not implemented yet.",
    ),
  );
}
function renderPrompt(root) {
  root.append(
    heading(
      "System prompt",
      "A versioned operating contract, saved with each new run.",
    ),
  );
  const area = el("div", "prompt");
  area.append(
    el(
      "p",
      "footnote",
      data.prompt.version +
        " · Default Docker/native-tool variant. Each run records its actual backend and tool catalog.",
    ),
    el("pre", "code", data.prompt.instructions),
    el("h3", "section-gap", "Native repository tools"),
  );
  const cards = el("div", "cards");
  for (const t of data.prompt.tools) {
    const c = el("article", "card");
    c.append(el("h3", "", t.name), el("p", "", t.description));
    cards.append(c);
  }
  area.append(cards);
  root.append(area);
}
function fillPreset() {
  const t = data.tasks[Number($("#preset").value)];
  if (!t) return;
  $("#goal").value = t.goal;
  $("#model").value = t.model;
  $("#context").value = t.limits.context_window_tokens || 200000;
  $("#output").value = t.limits.max_output_tokens;
  $("#usd").value = t.limits.max_usd;
  $("#usd").max = t.limits.max_usd;
  $("#preset-info").textContent =
    t.repository.split(/[\\/]/).pop() +
    " · " +
    (t.backend || "docker") +
    " · base " +
    short(t.ref);
  $("#run-budget-note").textContent =
    "Starting a run calls the real model. This task permits up to $" +
    t.limits.max_usd.toFixed(2) +
    " per run. The independent verifier and executor remain fixed by the prepared task.";
  const options = $("#skill-options");
  options.replaceChildren();
  for (const s of data.skills.filter((s) => s.repository === t.repository)) {
    const label = el("label", "check"),
      input = el("input");
    input.type = "checkbox";
    input.value = s.id;
    const selectedSkill = (t.skills || []).find((x) => x.id === s.id);
    input.dataset.version = selectedSkill?.version || s.version;
    input.checked = !!selectedSkill;
    label.append(
      input,
      el("span", "", s.id + " · " + short(input.dataset.version)),
    );
    options.append(label);
  }
  if (!options.children.length)
    options.append(el("p", "muted", "No active skills for this repository."));
}
let runChat = null;
let runRequestID = null;
function openRun() {
  runChat = null;
  runRequestID = null;
  $("#preset").disabled = false;
  $("#preset").replaceChildren();
  data.tasks.forEach((t, i) => {
    const o = el("option", "", t.name);
    o.value = i;
    $("#preset").append(o);
  });
  $("#form-error").textContent = "";
  fillPreset();
  $("#run-dialog").showModal();
}
$("#preset").addEventListener("change", fillPreset);
$("#close-dialog").addEventListener("click", () => $("#run-dialog").close());
$("#run-form").addEventListener("submit", async (e) => {
  e.preventDefault();
  $("#submit-run").disabled = true;
  $("#form-error").textContent = "";
  try {
    const payload = {
      ...(runChat ? { conversation: runChat, request_id: runRequestID } : {}),
      task: Number($("#preset").value),
      goal: $("#goal").value,
      model: $("#model").value,
      context_window_tokens: Number($("#context").value),
      max_output_tokens: Number($("#output").value),
      max_usd: Number($("#usd").value),
      skills: [
        ...document.querySelectorAll("#skill-options input:checked"),
      ].map((x) => ({ id: x.value, version: x.dataset.version })),
    };
    followNewRun = new Set(data.runs.map((x) => x.run.id));
    await api("runs", payload);
    $("#run-dialog").close();
    selected = null;
    detail = null;
    if (runChat) {
      chatDraft = "";
      if ($("#chat-message")) $("#chat-message").value = "";
    }
    await refresh();
  } catch (e) {
    $("#form-error").textContent = e.message;
  } finally {
    $("#submit-run").disabled = false;
  }
});
document.querySelectorAll("[data-view]").forEach((b) =>
  b.addEventListener("click", () => {
    view = b.dataset.view;
    render();
    refreshDetail();
  }),
);
async function refresh() {
  if (loading) return;
  loading = true;
  try {
    const latest = await api("overview");
    const changed = JSON.stringify(latest) !== JSON.stringify(data);
    data = latest;
    if (followNewRun) {
      const fresh = data.runs.find((x) => !followNewRun.has(x.run.id));
      if (fresh) {
        selected = fresh.run.id;
        followNewRun = null;
      }
    }
    notice(
      data.key_present
        ? ""
        : "Configure your key in Projects or with harness auth login to enable paid runs.",
    );
    if (changed || !$("#view").children.length) render();
    await refreshDetail();
    if (view === "chat") await refreshChat();
  } catch (e) {
    notice(e.message);
    if (!data)
      $("#view").replaceChildren(
        el(
          "p",
          "empty",
          "Open the private access link printed by harness serve.",
        ),
      );
  } finally {
    loading = false;
  }
}
document.addEventListener("DOMContentLoaded", () => {
  refresh();
  setInterval(refresh, 3000);
});
