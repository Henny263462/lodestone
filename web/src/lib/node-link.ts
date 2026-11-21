// Befehle, mit denen ein Node gestartet wird, der sich selbst zum Panel verbindet. Reine Funktionen ohne React.

export const nodeImage = "ghcr.io/henny263462/lodestone-node:latest";
export const defaultDataDir = "/srv/lodestone";

/** Erlaubte Adresse des Panels: http(s) oder ws(s), ohne Leerzeichen und Anführungszeichen (landet in Shell und YAML). */
export function validPanelUrl(value: string): boolean {
  return /^(https?|wss?):\/\/[A-Za-z0-9.\-[\]:]+(\/[A-Za-z0-9._~\-/]*)?$/.test(value.trim());
}

export function insecurePanelUrl(value: string): boolean {
  return /^(http|ws):\/\//i.test(value.trim());
}

/** Datenordner: absoluter Pfad ohne Sonderzeichen. Er muss auf Host und im Container derselbe sein. */
export function validDataDir(value: string): boolean {
  return /^\/[A-Za-z0-9._\-/]*$/.test(value.trim());
}

export interface NodeCommandInput {
  panelUrl: string;
  token: string;
  publicHost: string;
  dataDir: string;
}

function environment({ panelUrl, token, publicHost, dataDir }: NodeCommandInput): [string, string][] {
  const values: [string, string][] = [
    ["LODESTONE_NODE_PANEL_URL", panelUrl.trim().replace(/\/+$/, "")],
    ["LODESTONE_NODE_JOIN_TOKEN", token],
    ["LODESTONE_NODE_DATA_DIR", dataDir.trim()],
    ["LODESTONE_NODE_CONTAINER_USER", "1000:1000"],
  ];
  if (publicHost.trim()) values.push(["LODESTONE_NODE_PUBLIC_HOST", publicHost.trim()]);
  return values;
}

/** docker run: der Node braucht keinen offenen Port, nur Docker und den Datenordner (gleicher Pfad im Container). */
export function dockerRunCommand(input: NodeCommandInput): string {
  const dir = input.dataDir.trim();
  return [
    `mkdir -p ${dir} && sudo chown 1000:1000 ${dir}`,
    "docker network create lodestone 2>/dev/null || true",
    "docker run -d --name lodestone-node --restart unless-stopped \\",
    '  --user 1000:1000 --group-add "$(stat -c %g /var/run/docker.sock)" \\',
    "  --network lodestone \\",
    "  -v /var/run/docker.sock:/var/run/docker.sock \\",
    `  -v ${dir}:${dir} \\`,
    ...environment(input).map(([name, value]) => `  -e '${name}=${value}' \\`),
    `  ${nodeImage}`,
  ].join("\n");
}

/** docker-compose.yml mit demselben Inhalt. */
export function composeFile(input: NodeCommandInput): string {
  const dir = input.dataDir.trim();
  return [
    "services:",
    "  node:",
    `    image: ${nodeImage}`,
    "    restart: unless-stopped",
    '    user: "1000:1000"',
    "    group_add:",
    '      - "999" # Gruppe des Docker-Sockets: stat -c %g /var/run/docker.sock',
    "    environment:",
    ...environment(input).map(([name, value]) => `      ${name}: "${value}"`),
    "    volumes:",
    "      - /var/run/docker.sock:/var/run/docker.sock",
    `      - ${dir}:${dir}`,
    "    networks:",
    "      - lodestone",
    "networks:",
    "  lodestone:",
    "    name: lodestone",
  ].join("\n");
}
