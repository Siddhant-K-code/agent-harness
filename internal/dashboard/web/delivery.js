"use strict";
async function openDelivery(id) {
  let dialog = document.querySelector("#delivery-dialog");
  if (dialog) dialog.remove();
  dialog = el("dialog", "delivery-dialog");
  dialog.id = "delivery-dialog";
  const header = el("div", "dialog-head"),
    content = el("div", "delivery-content");
  header.append(
    el("h2", "", "Review GitHub delivery"),
    button("Close", "quiet", () => dialog.close()),
  );
  dialog.append(header, content);
  document.body.append(dialog);
  dialog.showModal();
  const endpoint = "runs/" + id + "/delivery";
  let current = null;
  const errorBox = el("p", "error");
  async function action(name, extra = {}) {
    errorBox.textContent = "";
    for (const b of content.querySelectorAll("button")) b.disabled = true;
    try {
      current = await api(endpoint, { action: name, ...extra });
      draw();
    } catch (e) {
      errorBox.textContent = e.message;
      try {
        current = await api(endpoint);
        draw(true);
      } catch (_) {
        /* keep original error */
      }
    } finally {
      for (const b of content.querySelectorAll("button")) b.disabled = false;
    }
  }
  function draw(keepError = false) {
    const message = keepError ? errorBox.textContent : "";
    content.replaceChildren();
    if (current) {
      const a = current.approval;
      content.append(
        badge(current.status),
        el("h3", "", a.title),
        el(
          "p",
          "",
          "As " +
            a.actor +
            " → " +
            a.repository +
            " · " +
            a.branch +
            " → " +
            a.base_branch,
        ),
      );
      content.append(
        el("p", "meta", "Verified base: " + a.base_commit),
        el("p", "meta", "Patch SHA-256: " + a.patch_sha256),
      );
      if (current.attention)
        content.append(el("p", "error", current.attention));
      if (current.pull_request) {
        const link = el(
          "a",
          "button primary",
          "Open pull request #" + current.pull_request.number,
        );
        // Server validates this URL against the approved github.com repository.
        link.href = current.pull_request.html_url;
        link.target = "_blank";
        link.rel = "noopener noreferrer";
        content.append(link);
      } else {
        content.append(
          el(
            "p",
            "muted",
            "Publishing pushes this commit and opens a draft PR using your host gh login. The agent never receives those credentials.",
          ),
        );
        if (current.patch) {
          content.append(
            el("pre", "code delivery-patch", current.patch),
            el("h3", "", "Pull request description"),
            el("pre", "code", a.body),
          );
        }
        const buttons = el("div", "project-actions");
        if (["prepared", "branch_pushed"].includes(current.status)) {
          buttons.append(
            button("Refresh approval preview", "quiet", () =>
              action("preview"),
            ),
          );
          if (current.patch)
            buttons.append(
              button("Approve & create draft PR", "primary", () =>
                action("publish", {
                  approval_hash: current.approval_hash,
                  approve: true,
                }),
              ),
            );
          else
            content.append(
              el(
                "p",
                "muted",
                "Refresh the preview to review the patch before approving.",
              ),
            );
        } else
          buttons.append(
            button("Reconcile GitHub status", "quiet", () =>
              action("reconcile"),
            ),
          );
        content.append(buttons);
      }
    } else {
      content.append(
        el(
          "p",
          "muted",
          "Prepare a preview of the exact verified patch, target repository and branch. This step reads GitHub and creates a local commit.",
        ),
        button("Prepare preview", "primary", () => action("preview")),
      );
    }
    errorBox.textContent = message;
    content.append(errorBox);
  }
  try {
    current = await api(endpoint);
  } catch (_) {
    /* no delivery yet */
  }
  draw();
}
