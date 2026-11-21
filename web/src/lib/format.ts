import { formatNumber, locale, t } from "@/lib/i18n";

export function bytes(value?: number | null): string {
  if (!value || isNaN(value)) return "0 MB";
  if (value >= 1024 ** 3) return `${formatNumber(value / 1024 ** 3, 1)} GB`;
  if (value >= 1024 ** 2) return `${formatNumber(value / 1024 ** 2, value >= 100 * 1024 ** 2 ? 0 : 1)} MB`;
  if (value >= 1024) return `${Math.round(value / 1024)} KB`;
  return `${value} B`;
}

export function megabytes(value: number): string {
  return value >= 1024 ? `${formatNumber(value / 1024, value % 1024 === 0 ? 0 : 1)} GB` : `${value} MB`;
}

export function dateTime(timestamp?: number | null): string {
  if (!timestamp || isNaN(timestamp)) return "–";
  return new Date(timestamp).toLocaleString(locale(), { dateStyle: "short", timeStyle: "short" });
}

export function ago(timestamp?: number | null): string {
  if (!timestamp || isNaN(timestamp)) return t("nie");
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000));
  if (seconds < 10) return t("gerade eben");
  if (seconds < 60) return t("vor {seconds} Sek", { seconds });
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return t("vor {minutes} Min", { minutes });
  const hours = Math.round(minutes / 60);
  if (hours < 48) return t("vor {hours} Std", { hours });
  return t("vor {days} Tagen", { days: Math.round(hours / 24) });
}

export function uptime(since?: number | null): string {
  if (!since) return "–";
  const minutes = Math.floor((Date.now() - since) / 60_000);
  if (minutes < 1) return t("gerade gestartet");
  if (minutes < 60) return t("{minutes} Min", { minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return t("{hours} Std {minutes} Min", { hours, minutes: minutes % 60 });
  return t("{days} Tage", { days: Math.floor(hours / 24) });
}

export function compact(value: number): string {
  if (value >= 1_000_000) return t("{value} Mio.", { value: formatNumber(value / 1_000_000, 1) });
  if (value >= 1_000) return t("{value} Tsd.", { value: formatNumber(value / 1_000, 1) });
  return String(value);
}

/** stateLabel ist die Beschriftung eines Serverzustands in der gültigen Sprache. */
export function stateLabel(state: string): string {
  switch (state) {
    case "installing":
      return t("Wird installiert");
    case "offline":
      return t("Aus");
    case "starting":
      return t("Startet");
    case "running":
      return t("Läuft");
    case "stopping":
      return t("Stoppt");
    case "restoring":
      return t("Wird wiederhergestellt");
    default:
      return state;
  }
}

export function stateTone(state?: string): "good" | "bad" | "warn" | "neutral" {
  switch (state) {
    case "running":
      return "good";
    case "starting":
    case "stopping":
    case "installing":
    case "restoring":
      return "warn";
    default:
      return "neutral";
  }
}

export function ansiStrip(text: string): string {
  // eslint-disable-next-line no-control-regex
  return text.replace(/\u001b\[[0-9;]*[A-Za-z]/g, "");
}

export function joinPath(dir: string, name: string): string {
  return dir === "/" || dir === "" ? name : `${dir.replace(/\/+$/, "")}/${name}`;
}

export function parentPath(path: string): string {
  const parts = path.split("/").filter(Boolean);
  parts.pop();
  return parts.join("/");
}
