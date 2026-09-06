"use strict";
let inspectedProject = null;
function projectInput(id, value = "", placeholder = "") {
  const input = el("input");
  Object.assign(input, { id, value, placeholder, required: true });
  return input;
}
function renderProjects(root) {
  if (!$("#projects-screen")) {
    root.replaceChildren(
      heading(
        "Projects",
        "Bring your repository. Set a budget and the checks that define success.",
      ),
    );
    const screen = el("div", "project-layout");
    screen.id = "projects-screen";
    const form = el("form", "panel project-form");
    form.id = "project-form";
    const path = projectInput(
      "project-path",
      "",
      "/absolute/path/to/repository",
    );
    const ref = projectInput("project-ref", "HEAD");
    const inspect = button("Inspect repository", "quiet", async () => {
      inspect.disabled = true;
      $("#project-error").textContent = "";
      try {
        inspectedProject = await api("projects/inspect", {
          repository: path.value,
          ref: ref.value,
        });
        $("#project-preview").textContent =
          inspectedProject.name +
          " · commit " +
          short(inspectedProject.commit) +
          (inspectedProject.dirty
            ? " · Uncommitted changes will be excluded."
            : " · Working tree is clean.");
        $("#project-create").disabled = false;
      } catch (e) {
        inspectedProject = null;
        $("#project-create").disabled = true;
        $("#project-error").textContent = e.message;
      } finally {
        inspect.disabled = false;
      }
    });
    for (const input of [path, ref])
      input.addEventListener("input", () => {
        inspectedProject = null;
        $("#project-create").disabled = true;
        $("#project-preview").textContent =
          "Inspect the selected revision before saving.";
      });
    const row = el("div", "form-grid");
    row.append(field("Local repository", path), field("Revision", ref));
    const preview = el("p", "muted", "Only committed source is used.");
    preview.id = "project-preview";
    const goal = el("textarea");
    Object.assign(goal, {
      id: "project-goal",
      rows: 3,
      maxLength: 32000,
      required: true,
      placeholder: "Describe the change and its expected behavior…",
    });
    const image = projectInput(
      "project-image",
      "",
      "Prepared Docker image, e.g. my-project:dev",
    );
    const verifier = el("textarea");
    Object.assign(verifier, {
      id: "project-verifier",
      rows: 7,
      maxLength: 32000,
      required: true,
      placeholder:
        "set -eu\n# Add assertions that establish the requested behavior.",
    });
    const model = el("select");
    model.id = "project-model";
    for (const m of data.models) {
      const option = el("option", "", m.id);
      model.append(option);
    }
    const settings = el("div", "form-grid");
    settings.append(
      field("Model", model),
      field(
        "Maximum per run ($)",
        chatNumber("project-usd", 0.5, 0.01, 100, "0.01"),
      ),
      field(
        "Context window",
        chatNumber("project-context", 32768, 1024, 1050000),
      ),
      field("Reserved output", chatNumber("project-output", 2048, 256, 128000)),
    );
    const error = el("p", "error");
    error.id = "project-error";
    error.setAttribute("role", "alert");
    const save = el("button", "primary", "Save project");
    save.id = "project-create";
    save.type = "submit";
    save.disabled = true;
    form.append(
      el("h2", "", "Add a project"),
      row,
      inspect,
      preview,
      field("Task goal", goal),
      field("Execution image", image),
      el(
        "p",
        "muted",
        "Build or pull the image first, with your dependencies installed. Agent commands run without network access.",
      ),
      field("Independent verification script", verifier),
      el(
        "p",
        "muted",
        "This script is stored outside the agent workspace and runs there read-only. Keep critical assertions here: the agent can edit tests inside the repository.",
      ),
      settings,
      error,
      save,
    );
    form.addEventListener("submit", async (e) => {
      e.preventDefault();
      if (!inspectedProject) return;
      save.disabled = true;
      error.textContent = "";
      try {
        const created = await api("projects", {
          repository: inspectedProject.repository,
          ref: inspectedProject.commit,
          goal: goal.value,
          image: image.value,
          verifier_script: verifier.value,
          model: model.value,
          max_usd: Number($("#project-usd").value),
          context_window_tokens: Number($("#project-context").value),
          max_output_tokens: Number($("#project-output").value),
        });
        await refresh();
        notice("Project saved. Checking readiness without model requests…");
        await checkProjectReadiness(created.task_index);
      } catch (e) {
        error.textContent = e.message;
      } finally {
        save.disabled = !inspectedProject;
      }
    });
    const sidebar = el("section", "project-side");
    const key = el("section", "panel project-form");
    key.append(el("h2", "", "Your OpenAI key"));
    const keyStatus = el("p", "muted");
    keyStatus.id = "project-key-status";
    const keyForm = el("form");
    keyForm.id = "project-key-form";
    const keyInput = projectInput("project-key");
    keyInput.type = "password";
    keyInput.autocomplete = "off";
    keyInput.maxLength = 4096;
    keyInput.spellcheck = false;
    const keySave = el("button", "quiet", "Save key locally");
    keySave.type = "submit";
    keyForm.append(
      field("API key", keyInput),
      el(
        "p",
        "muted",
        "Sent only to this authenticated local server. Stored in an owner-only plaintext file on your computer; never returned to the browser.",
      ),
      keySave,
    );
    keyForm.addEventListener("submit", async (e) => {
      e.preventDefault();
      keySave.disabled = true;
      try {
        await api("credentials", { key: keyInput.value });
        keyInput.value = "";
        await refresh();
      } catch (e) {
        notice(e.message);
      } finally {
        keyInput.value = "";
        keySave.disabled = false;
      }
    });
    key.append(keyStatus, keyForm);
    const projects = el("section", "panel project-form");
    projects.append(el("h2", "", "Saved projects"));
    const list = el("div");
    list.id = "project-list";
    projects.append(list);
    const readiness = el("section", "panel project-form");
    readiness.id = "project-readiness";
    readiness.append(
      el(
        "p",
        "muted",
        "Check a project to see what is ready and what needs attention.",
      ),
    );
    sidebar.append(key, projects, readiness);
    screen.append(form, sidebar);
    root.append(screen);
  }
  $("#project-key-status").textContent = data.key_present
    ? "Configured through " +
      data.key_source +
      ". Key presence checked; API access is not validated here."
    : "No working key configuration found.";
  $("#project-key-form").hidden = data.key_present;
  const list = $("#project-list");
  list.replaceChildren();
  (data.tasks || []).forEach((task, index) => {
    const item = el("div", "project-item");
    item.append(
      el("strong", "", task.name),
      el("p", "meta", task.repository),
      el(
        "p",
        "meta",
        task.model + " · " + money(task.limits.max_usd) + " maximum per run",
      ),
    );
    item.append(
      button("Check readiness", "quiet", () => checkProjectReadiness(index)),
      button("Open chat", "quiet", () => {
        chatPreset = index;
        chatID = null;
        chatData = null;
        sessionStorage.removeItem("harness-chat");
        view = "chat";
        render();
      }),
    );
    list.append(item);
  });
  if (!data.tasks?.length)
    list.append(
      el("p", "muted", "Save your first project to start a conversation."),
    );
}
async function checkProjectReadiness(index) {
  const box = $("#project-readiness");
  if (!box) return;
  box.replaceChildren(el("p", "pending", "Checking project readiness…"));
  try {
    const result = await api("projects/check", { task: index });
    box.replaceChildren(
      el("h2", "", result.ready ? "Ready to run" : "Setup needs attention"),
    );
    for (const check of result.checks) {
      const row = el("div", "check-row");
      row.append(
        el(
          "strong",
          check.ok ? "check-ok" : "error",
          (check.ok ? "✓ " : "! ") + check.name,
        ),
        el("p", "meta", check.detail),
      );
      box.append(row);
    }
    box.append(
      el("p", "muted", "No model call or verifier execution was performed."),
    );
  } catch (e) {
    box.replaceChildren(el("p", "error", e.message));
  }
}
