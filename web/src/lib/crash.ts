// Erkennt bekannte Fehlerbilder in den letzten Zeilen der Konsole und erklärt, was zu tun ist.

export interface Hint {
  id: string;
  level: "warn" | "bad";
  title: string;
  text: string;
}

// --- automatik: Details aus Absturzbericht und Log (verdächtige Mod, fehlende Abhängigkeit, Java-Version, Mixin)

/** Mods, die der Absturzbericht unter „Suspected Mod(s)“ nennt (Forge/NeoForge, Fabric mit Zusatzmods). */
export function suspectedMods(text: string): string[] {
  const block = /Suspected Mods?(?:\(s\))?:([\s\S]{0,800})/.exec(text)?.[1].split(/\n\s*\n/)[0] ?? "";
  const found: string[] = [];
  for (const match of block.matchAll(/([^\t\n(]{1,80}?) \(([a-z][a-z0-9_-]{1,63})\), Version: ([^\s,]+)/g)) {
    const name = match[1].trim();
    found.push(name && name !== match[2] ? `${name} (${match[2]})` : match[2]);
  }
  return [...new Set(found)].slice(0, 3);
}

/** Fehlende Abhängigkeiten als „fehlt – gebraucht von“. */
export function missingDependencies(text: string): { requires: string; mod: string }[] {
  const found: { requires: string; mod: string }[] = [];
  for (const match of text.matchAll(/Mod '[^']{1,80}' \(([a-z][a-z0-9_-]{1,63})\) \S+ requires (?:version \S+(?: or later)? |any version )of (?:mod )?'?([^',()]{1,80}?)'?(?: \(([a-z][a-z0-9_-]{1,63})\))?, which is missing/g)) {
    found.push({ mod: match[1], requires: match[3] ?? match[2].trim() });
  }
  for (const match of text.matchAll(/Mod ID: '([a-z][a-z0-9_-]{1,63})', Requested by: '([a-z][a-z0-9_-]{1,63})'/g)) {
    found.push({ mod: match[2], requires: match[1] });
  }
  return found.filter((entry, index) => found.findIndex((other) => other.mod === entry.mod && other.requires === entry.requires) === index).slice(0, 3);
}

/** Gebrauchte und laufende Java-Version aus „class file version 65.0 … up to 61.0“. */
export function javaVersions(text: string): { required: number; current: number } | null {
  const match = /class file version (\d{2})\.0\)?,? this version of the Java Runtime only recognizes class file versions up to (\d{2})\.0/.exec(text);
  return match ? { required: Number(match[1]) - 44, current: Number(match[2]) - 44 } : null;
}

/** Mixin-Konfigurationen, die beim Anwenden gescheitert sind (heißen fast immer wie die Mod). */
export function failedMixins(text: string): string[] {
  const found: string[] = [];
  for (const match of text.matchAll(/Mixin \[([a-z][a-z0-9_-]{1,63}\.mixins?\.json:[A-Za-z0-9_.$]{1,120})\]/g)) found.push(match[1]);
  for (const match of text.matchAll(/Mixin apply (?:for mod \S+ )?failed ([a-z][a-z0-9_-]{1,63}\.mixins?\.json:[A-Za-z0-9_.$]{1,120})/g)) found.push(match[1]);
  return [...new Set(found)].slice(0, 2);
}

const quote = (list: string[]) => list.map((entry) => `„${entry}“`).join(", ");

const rules: { id: string; level: Hint["level"]; pattern: RegExp; title: string; text: string; detail?: (text: string) => string | null }[] = [
  {
    id: "suspect",
    level: "bad",
    pattern: /Suspected Mods?(?:\(s\))?:\s*(?!NONE)\S/,
    title: "Der Absturzbericht nennt eine verdächtige Mod",
    text: "Der Bericht in crash-reports/ zeigt auf die Mod, in der der Fehler passiert ist. Aktualisiere oder entferne sie unter Mods und starte neu.",
    detail: (text) => {
      const mods = suspectedMods(text);
      return mods.length > 0 ? `Verdächtig: ${quote(mods)}.` : null;
    },
  },
  {
    id: "mixin",
    level: "bad",
    pattern: /MixinApplyError|InvalidMixinException|InvalidInjectionException|MixinTransformerError|Mixin apply (?:for mod \S+ )?failed/,
    title: "Mixin-Fehler einer Mod",
    text: "Eine Mod verändert Minecraft-Code, der in dieser Version anders aussieht. Meist hilft ein Update der Mod oder die Minecraft-Version, für die sie gebaut ist.",
    detail: (text) => {
      const mixins = failedMixins(text);
      return mixins.length > 0 ? `Betroffen: ${quote(mixins)} (Mod „${mixins[0].split(".")[0]}“).` : null;
    },
  },
  {
    id: "oom",
    level: "bad",
    pattern: /OutOfMemoryError|Java heap space|Exit-Code 137|GC overhead limit/,
    title: "Zu wenig Arbeitsspeicher",
    text: "Der Server hat mehr gebraucht, als sein Limit erlaubt (Exit-Code 137 heißt: vom System beendet). Gib ihm unter Einstellungen mehr Arbeitsspeicher. Paper 26 und Modpacks brauchen schnell 2 bis 4 GB.",
  },
  {
    id: "eula",
    level: "bad",
    pattern: /agree to the EULA|eula=false/i,
    title: "Die Minecraft-EULA ist nicht akzeptiert",
    text: "Öffne unter Dateien die Datei eula.txt und setze eula=true. Danach starten.",
  },
  {
    id: "port",
    level: "bad",
    pattern: /Address already in use|Failed to bind to port|BindException/,
    title: "Der Port ist schon belegt",
    text: "Ein anderes Programm nutzt denselben Port. Prüfe unter Einstellungen die Anschlüsse oder stoppe den anderen Server.",
  },
  {
    id: "java",
    level: "bad",
    pattern: /UnsupportedClassVersionError|class file version|requires Java|Unsupported Java/,
    title: "Falsche Java-Version",
    text: "Software und Java passen nicht zusammen. Wechsle unter Einstellungen die Version oder leg das Docker-Image fest, das zur Software passt.",
    detail: (text) => {
      const java = javaVersions(text);
      return java ? `Gebraucht wird Java ${java.required}, es läuft Java ${java.current}.` : null;
    },
  },
  {
    id: "plugin",
    level: "warn",
    pattern: /Could not load '[^']+\.jar'|Error occurred while enabling|Unsupported API version|Plugin .* requires/,
    title: "Ein Plugin lässt sich nicht laden",
    text: "Meist passt die Plugin-Version nicht zur Server-Version, oder eine Abhängigkeit fehlt. Schau unter Plugins nach Updates oder deaktiviere das Plugin.",
  },
  {
    id: "mods",
    level: "bad",
    pattern: /Incompatible mod set|Missing or unsupported mandatory dependencies|Mod resolution failed|Incompatible mods found|, which is missing/,
    title: "Mods passen nicht zusammen",
    text: "Eine Mod fehlt oder ist für eine andere Version gebaut. Die Zeilen darüber nennen, welche. Entferne oder aktualisiere sie unter Mods.",
    detail: (text) => {
      const missing = missingDependencies(text);
      return missing.length > 0 ? `Es fehlt: ${missing.map((entry) => `„${entry.requires}“ (gebraucht von „${entry.mod}“)`).join(", ")}.` : null;
    },
  },
  {
    id: "forwarding",
    level: "warn",
    pattern: /enable it in your BungeeCord config|Unable to verify player details|ip_forward|Velocity forwarding|modern forwarding/i,
    title: "Der Server erwartet den Proxy",
    text: "Spieler müssen über den Proxy beitreten. Im Projekt unter Netzwerk „Neu abgleichen“ drücken und den Server neu starten, damit die Weiterleitung eingerichtet ist.",
  },
  {
    id: "permission",
    level: "bad",
    pattern: /AccessDeniedException|Permission denied/,
    title: "Fehlende Dateirechte",
    text: "Der Server darf in einen Ordner nicht schreiben. Der Datenordner des Nodes muss dem Benutzer 1000:1000 gehören (chown -R 1000:1000).",
  },
  {
    id: "lag",
    level: "warn",
    pattern: /Can't keep up!|Is the server overloaded/,
    title: "Der Server kommt nicht hinterher",
    text: "Die Tick-Rate sinkt. Gib ihm mehr Arbeitsspeicher oder CPU, senke die Sichtweite (view-distance) oder entferne aufwendige Plugins.",
  },
];

/** Prüft die letzten Zeilen und liefert höchstens drei Hinweise, die schlimmsten zuerst. */
export function analyse(lines: string[]): Hint[] {
  const text = lines.slice(-400).join("\n");
  const found = rules
    .filter((rule) => rule.pattern.test(text))
    .map(({ id, level, title, text: body, detail }) => {
      const extra = detail?.(text);
      return { id, level, title, text: extra ? `${body} ${extra}` : body };
    });
  found.sort((a, b) => Number(b.level === "bad") - Number(a.level === "bad"));
  return found.slice(0, 3);
}
