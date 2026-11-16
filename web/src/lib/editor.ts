// Reine Hilfen zum Datei-Editor. Getrennt vom Editor selbst, damit CodeMirror erst geladen wird, wenn jemand eine Datei öffnet.

export type EditorLanguage = "json" | "yaml" | "xml" | "markdown" | "properties" | "toml" | "shell" | "text";

/** Wählt die Sprache für die Syntaxhervorhebung nach dem Dateinamen. */
export function languageFor(name: string): EditorLanguage {
  const lower = name.toLowerCase();
  const extension = lower.includes(".") ? lower.slice(lower.lastIndexOf(".") + 1) : "";
  switch (extension) {
    case "json":
    case "json5":
    case "mcmeta":
      return "json";
    case "yml":
    case "yaml":
      return "yaml";
    case "xml":
    case "html":
    case "svg":
      return "xml";
    case "md":
      return "markdown";
    case "properties":
    case "cfg":
    case "conf":
    case "ini":
    case "lang":
    case "secret":
      return "properties";
    case "toml":
      return "toml";
    case "sh":
    case "bat":
    case "cmd":
      return "shell";
  }
  return "text";
}

const textExtensions = new Set(["txt", "log", "json", "json5", "mcmeta", "yml", "yaml", "xml", "html", "md", "properties", "cfg", "conf", "ini", "lang", "toml", "sh", "bat", "cmd", "csv", "secret", "js", "ts", "css", "sk", "mcfunction", "env", "list", "txt~", "gradle", "json_old", "old", "bak"]);

/** Dateien, die sich als Text bearbeiten lassen (alles andere kann man nur herunterladen). */
export function isEditable(name: string, size: number): boolean {
  if (size > 5 * 1024 * 1024) return false;
  const lower = name.toLowerCase();
  if (!lower.includes(".")) return true; // z. B. "eula" oder "Dockerfile"
  return textExtensions.has(lower.slice(lower.lastIndexOf(".") + 1)) || lower.endsWith(".json.disabled") || lower.endsWith(".yml.disabled");
}
