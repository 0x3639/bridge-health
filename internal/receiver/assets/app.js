"use strict";
const installation = document.getElementById("installation");
if (installation) {
  const token = window.location.hash.slice(1);
  if (/^[a-f0-9-]{36}\.[A-Za-z0-9_-]{43}$/.test(token)) {
    const origin = installation.dataset.origin;
    document.getElementById("install-command").value =
      `installer=$(curl -fsS '${origin}/install.py') && printf '%s\\n' '${token}' | sudo python3 -I -c "$installer"`;
    document.getElementById("invalid-invite").hidden = true;
    document.getElementById("valid-invite").hidden = false;
  }
}
document.querySelectorAll("[data-copy]").forEach((button) => {
  button.addEventListener("click", async () => {
    const field = document.getElementById(button.dataset.copy);
    try {
      await navigator.clipboard.writeText(field.value);
      const label = button.textContent;
      button.textContent = "Copied";
      document.getElementById("copy-status").textContent =
        label + " succeeded.";
      setTimeout(() => {
        button.textContent = label;
      }, 1800);
    } catch {
      field.focus();
      field.select();
      document.getElementById("copy-status").textContent =
        "Selected for copying.";
    }
  });
});
document.querySelectorAll("form[data-confirm]").forEach((form) => {
  form.addEventListener("submit", (event) => {
    if (!window.confirm(form.dataset.confirm)) event.preventDefault();
  });
});
