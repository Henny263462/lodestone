// Passkeys im Browser (navigator.credentials) ohne zusätzliche Bibliothek. Das Panel schickt die Optionen als JSON mit
// base64url-Feldern; der Browser braucht dort ArrayBuffer und liefert ArrayBuffer zurück, die wir wieder als base64url senden.

import type { PasskeyAssertionOptions, PasskeyCreationOptions } from "@/lib/types";

export function toBase64Url(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

export function fromBase64Url(text: string): ArrayBuffer {
  const base64 = text.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(text.length / 4) * 4, "=");
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index++) bytes[index] = binary.charCodeAt(index);
  return bytes.buffer;
}

/** Ob der Browser Passkeys kann (sicherer Kontext: HTTPS oder localhost). */
export function passkeysSupported(): boolean {
  return typeof window !== "undefined" && window.isSecureContext && typeof window.PublicKeyCredential === "function" && !!navigator.credentials;
}

/** Deutsche Meldung für die typischen Abbrüche des Browsers. */
export function passkeyErrorText(error: unknown): string {
  if (error instanceof DOMException) {
    if (error.name === "NotAllowedError") return "Der Vorgang wurde abgebrochen oder ist abgelaufen. Versuch es noch einmal.";
    if (error.name === "InvalidStateError") return "Dieser Passkey ist schon für dein Konto registriert.";
    if (error.name === "SecurityError") return "Die Adresse passt nicht zur Einstellung des Panels (LODESTONE_PUBLIC_URL). Öffne das Panel über diese Adresse.";
    if (error.name === "NotSupportedError") return "Dein Browser oder Gerät unterstützt diese Art Passkey nicht.";
  }
  return error instanceof Error ? error.message : "Der Passkey hat nicht geantwortet. Versuch es noch einmal.";
}

function descriptors(list?: { type: string; id: string; transports?: string[] }[]): PublicKeyCredentialDescriptor[] | undefined {
  return list?.map((entry) => ({ type: "public-key", id: fromBase64Url(entry.id), transports: entry.transports as AuthenticatorTransport[] | undefined }));
}

/** Legt einen Passkey an und liefert die Antwort für /me/passkeys/finish. */
export async function createPasskey(options: PasskeyCreationOptions): Promise<unknown> {
  const source = options.publicKey;
  const publicKey: PublicKeyCredentialCreationOptions = {
    ...(source as unknown as PublicKeyCredentialCreationOptions),
    challenge: fromBase64Url(source.challenge),
    user: { ...source.user, id: fromBase64Url(source.user.id) },
    excludeCredentials: descriptors(source.excludeCredentials),
  };
  const credential = (await navigator.credentials.create({ publicKey })) as PublicKeyCredential | null;
  if (!credential) throw new Error("Der Browser hat keinen Passkey angelegt.");
  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    id: credential.id,
    rawId: toBase64Url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      clientDataJSON: toBase64Url(response.clientDataJSON),
      attestationObject: toBase64Url(response.attestationObject),
      transports: typeof response.getTransports === "function" ? response.getTransports() : undefined,
    },
  };
}

/** Fragt einen Passkey ab und liefert die Antwort für die Anmeldung. */
export async function getPasskey(options: PasskeyAssertionOptions): Promise<unknown> {
  const source = options.publicKey;
  const publicKey: PublicKeyCredentialRequestOptions = {
    ...(source as unknown as PublicKeyCredentialRequestOptions),
    challenge: fromBase64Url(source.challenge),
    allowCredentials: descriptors(source.allowCredentials),
  };
  const credential = (await navigator.credentials.get({ publicKey })) as PublicKeyCredential | null;
  if (!credential) throw new Error("Der Browser hat keinen Passkey geliefert.");
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    id: credential.id,
    rawId: toBase64Url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults(),
    response: {
      clientDataJSON: toBase64Url(response.clientDataJSON),
      authenticatorData: toBase64Url(response.authenticatorData),
      signature: toBase64Url(response.signature),
      userHandle: response.userHandle ? toBase64Url(response.userHandle) : undefined,
    },
  };
}
