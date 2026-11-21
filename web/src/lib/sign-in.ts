import { t } from "@/lib/i18n";
// Fehlercodes, mit denen das Panel nach einer Anmeldung über OIDC zurückleitet (/login/?error=…, /account/?oidc=…).
// Die Adresse trägt nur den Code; der Satz dazu steht hier.

const messages = (): Record<string, string> => ({
  oidc_off: t("Die Anmeldung über einen Anbieter ist auf diesem Panel nicht eingerichtet."),
  oidc_unavailable: t("Der Anmeldedienst ist gerade nicht erreichbar. Versuch es später noch einmal."),
  oidc_state: t("Die Anmeldung ist abgelaufen oder kam aus einem anderen Browserfenster. Starte sie noch einmal."),
  oidc_denied: t("Die Anmeldung wurde beim Anbieter abgebrochen."),
  oidc_invalid: t("Die Antwort des Anbieters ließ sich nicht prüfen. Starte die Anmeldung noch einmal; hält das an, prüfe die OIDC-Einstellungen."),
  oidc_unlinked: t("Dieses Konto ist mit keinem Benutzer verknüpft. Melde dich mit deinem Passwort an und verknüpfe es unter Konto, oder bitte einen Administrator darum."),
  oidc_taken: t("Dieses Konto beim Anbieter ist schon mit einem anderen Benutzer verknüpft."),
  oidc_failed: t("Die Anmeldung über den Anbieter hat nicht geklappt. Versuch es noch einmal."),
  disabled: t("Dein Benutzer ist gesperrt. Wende dich an einen Administrator."),
  throttled: t("Zu viele Fehlversuche. Warte ein paar Minuten und versuch es dann noch einmal."),
});

export function signInErrorText(code: string): string {
  return messages()[code] ?? t("Die Anmeldung hat nicht geklappt. Versuch es noch einmal.");
}
