// Übernimmt die gespeicherte Darstellung (Konto → Darstellung), bevor die Seite gezeichnet wird.
// Gleiche Schlüssel und Werte wie src/lib/appearance.ts.
(function () {
  try {
    var root = document.documentElement;
    var font = localStorage.getItem("lodestone.font");
    var accent = localStorage.getItem("lodestone.accent");
    if (font === "minecraft" || font === "pixel" || font === "modern" || font === "system") root.dataset.font = font;
    if (accent === "emerald" || accent === "diamond" || accent === "lapis" || accent === "amethyst" || accent === "copper") root.dataset.accent = accent;
  } catch (error) {
    // Ohne Speicherzugriff bleibt die Standarddarstellung
  }
})();
