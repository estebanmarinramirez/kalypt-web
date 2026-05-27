const form = document.querySelector("#contact-form");
const statusBox = document.querySelector("#form-status");

if (form && statusBox) {
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    statusBox.textContent = "Sending...";

    try {
      const response = await fetch(form.action, {
        method: "POST",
        body: new FormData(form),
        headers: { Accept: "application/json" },
      });
      const data = await response.json();
      statusBox.textContent = data.message || "Response received.";
      statusBox.dataset.state = response.ok ? "ok" : "error";
      if (response.ok) form.reset();
    } catch {
      statusBox.textContent = "Network error. Please try again.";
      statusBox.dataset.state = "error";
    }
  });
}
