package main

import (
	"strings"

	"github.com/lodestone-mc/lodestone/internal/panel"
)

// ssoConfig liest die Einstellungen für die Anmeldung über OIDC und Passkeys aus der Umgebung.
func ssoConfig() panel.SSOConfig {
	return panel.SSOConfig{
		PublicURL:        env("LODESTONE_PUBLIC_URL", ""),
		OIDCIssuer:       env("LODESTONE_OIDC_ISSUER", ""),
		OIDCClientID:     env("LODESTONE_OIDC_CLIENT_ID", ""),
		OIDCClientSecret: env("LODESTONE_OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:  env("LODESTONE_OIDC_REDIRECT_URL", ""),
		OIDCName:         env("LODESTONE_OIDC_NAME", ""),
		OIDCScopes:       strings.Fields(strings.ReplaceAll(env("LODESTONE_OIDC_SCOPES", ""), ",", " ")),
		OIDCAutoCreate:   flag("LODESTONE_OIDC_AUTO_CREATE"),
		OIDCAdminGroup:   env("LODESTONE_OIDC_ADMIN_GROUP", ""),
		OIDCGroupsClaim:  env("LODESTONE_OIDC_GROUPS_CLAIM", ""),
		OIDCOnly:         flag("LODESTONE_OIDC_ONLY"),
		PasskeyRPID:      env("LODESTONE_WEBAUTHN_RP_ID", ""),
		PasskeyOrigins:   strings.Fields(strings.ReplaceAll(env("LODESTONE_WEBAUTHN_ORIGINS", ""), ",", " ")),
	}
}
