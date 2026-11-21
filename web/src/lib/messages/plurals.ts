// Texte, die sich mit der Zahl ändern („1 Datei“, „3 Dateien“). {count} ist die Zahl, weitere Platzhalter wie bei t().
// Jede Sprache hat ihre eigenen Formen; die Auswahl macht Intl.PluralRules (tp in lib/i18n.ts). Ein neuer Eintrag in
// pluralsDe braucht denselben Schlüssel in pluralsEn, sonst meldet tsc einen Fehler.

export type PluralForms = { one: string; other: string };

export const pluralsDe = {
  recoveryCodesLeft: { one: "Noch {count} Wiederherstellungscode übrig.", other: "Noch {count} Wiederherstellungscodes übrig." },
  updatesAvailable: { one: "{count} Update verfügbar", other: "{count} Updates verfügbar" },
  filesUploaded: { one: "{count} Datei hochgeladen", other: "{count} Dateien hochgeladen" },
  moveEntries: { one: "{count} Eintrag verschieben", other: "{count} Einträge verschieben" },
  deleteEntries: { one: "{count} Eintrag löschen?", other: "{count} Einträge löschen?" },
  changesPending: { one: "{count} Änderung – gilt nach dem nächsten Neustart", other: "{count} Änderungen – gilt nach dem nächsten Neustart" },
  datapacks: { one: "{count} Datapack", other: "{count} Datapacks" },
  networks: { one: "{count} Netzwerk", other: "{count} Netzwerke" },
  tunnelServers: {
    one: "{names} hat eine öffentliche Adresse (z. B. einen playit-Tunnel). Sie gehört zur alten Maschine und wird beim Umzug entfernt. Lege danach auf dem neuen Node einen neuen Tunnel an.",
    other: "{names} haben eine öffentliche Adresse (z. B. einen playit-Tunnel). Sie gehört zur alten Maschine und wird beim Umzug entfernt. Lege danach auf dem neuen Node einen neuen Tunnel an.",
  },
  worldsDeleted: {
    one: "{lost} wird gelöscht – mit allen Bauten, Inventaren und Fortschritten darin. {backup}",
    other: "{lost} werden gelöscht – mit allen Bauten, Inventaren und Fortschritten darin. {backup}",
  },
} satisfies Record<string, PluralForms>;

export type PluralKey = keyof typeof pluralsDe;

export const pluralsEn: Record<PluralKey, PluralForms> = {
  recoveryCodesLeft: { one: "{count} recovery code left.", other: "{count} recovery codes left." },
  updatesAvailable: { one: "{count} update available", other: "{count} updates available" },
  filesUploaded: { one: "{count} file uploaded", other: "{count} files uploaded" },
  moveEntries: { one: "Move {count} item", other: "Move {count} items" },
  deleteEntries: { one: "Delete {count} item?", other: "Delete {count} items?" },
  changesPending: { one: "{count} change – takes effect after the next restart", other: "{count} changes – take effect after the next restart" },
  datapacks: { one: "{count} datapack", other: "{count} datapacks" },
  networks: { one: "{count} network", other: "{count} networks" },
  tunnelServers: {
    one: "{names} has a public address (e.g. a playit tunnel). It belongs to the old machine and is removed during the move. Afterwards, create a new tunnel on the new node.",
    other: "{names} have a public address (e.g. a playit tunnel). It belongs to the old machine and is removed during the move. Afterwards, create a new tunnel on the new node.",
  },
  worldsDeleted: {
    one: "{lost} will be deleted – with all builds, inventories and progress in it. {backup}",
    other: "{lost} will be deleted – with all builds, inventories and progress in them. {backup}",
  },
};
