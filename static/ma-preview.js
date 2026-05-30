function formatClock(timeZone) {
  try {
    return new Intl.DateTimeFormat("en-GB", {
      timeZone,
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).format(new Date());
  } catch {
    return "--:--";
  }
}

function updatePreviewClocks() {
  const tokyo = document.querySelector("#tko");
  const berlin = document.querySelector("#ber");
  const newYork = document.querySelector("#nyc");
  if (tokyo) tokyo.textContent = formatClock("Asia/Tokyo");
  if (berlin) berlin.textContent = formatClock("Europe/Berlin");
  if (newYork) newYork.textContent = formatClock("America/New_York");
}

updatePreviewClocks();
window.setInterval(updatePreviewClocks, 1000);

if ("IntersectionObserver" in window) {
  const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {
      if (!entry.isIntersecting) return;
      entry.target.classList.add("in");
      observer.unobserve(entry.target);
    });
  }, { threshold: 0.12 });

  document.querySelectorAll(".reveal").forEach((element) => observer.observe(element));
} else {
  document.querySelectorAll(".reveal").forEach((element) => element.classList.add("in"));
}
