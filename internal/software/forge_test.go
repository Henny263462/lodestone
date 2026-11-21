package software

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeForge spielt das Maven und die Promotions von Forge nach.
func fakeForge(t *testing.T) (*Resolver, *httptest.Server, []byte) {
	t.Helper()
	installer := []byte("forge installer")
	sum := sha256.Sum256(installer)
	mux := http.NewServeMux()
	mux.HandleFunc("/forge/maven-metadata.xml", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<metadata><versioning><versions>
<version>26.3-66.1.9</version>
<version>1.21.1-52.1.21</version>
<version>1.21.1-52.1.20</version>
<version>1.20.1-47.4.29</version>
<version>1.20.1-47.4.28</version>
<version>1.20.1-47.4.0</version>
<version>1.12.2-14.23.5.2860</version>
<version>1.7.10-10.13.4.1614-1.7.10</version>
<version>1.5.2-7.8.1.738</version>
</versions></versioning></metadata>`)
	})
	mux.HandleFunc("/promotions.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"promos":{"1.20.1-recommended":"47.4.28","1.20.1-latest":"47.4.29","1.21.1-latest":"52.1.21"}}`)
	})
	mux.HandleFunc("/forge/1.20.1-47.4.28/forge-1.20.1-47.4.28-installer.jar", func(w http.ResponseWriter, _ *http.Request) { w.Write(installer) })
	mux.HandleFunc("/forge/1.20.1-47.4.28/forge-1.20.1-47.4.28-installer.jar.sha256", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, hex.EncodeToString(sum[:]))
	})
	mux.HandleFunc("/forge/1.21.1-52.1.21/forge-1.21.1-52.1.21-installer.jar.sha256", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, strings.Repeat("ab", 32)+"  forge-1.21.1-52.1.21-installer.jar\n")
	})
	mux.HandleFunc("/forge/1.20.1-47.4.0/forge-1.20.1-47.4.0-installer.jar.sha256", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "keine prüfsumme")
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	resolver := &Resolver{HTTP: server.Client(), MojangManifest: server.URL + "/mojang", ForgeMaven: server.URL + "/forge", ForgePromotions: server.URL + "/promotions.json"}
	return resolver, server, installer
}

func TestForgeVersions(t *testing.T) {
	resolver, _, _ := fakeForge(t)
	versions, err := resolver.Versions(context.Background(), Forge, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[26.3 1.21.1 1.20.1 1.12.2]"; fmt.Sprint(versions) != want {
		t.Fatalf("Forge-Versionen = %v, erwartet %s (ab 1.12, ohne Doppelte)", versions, want)
	}
	if info, ok := Lookup(Forge); !ok || info.Extensions != ExtMods || info.Kind != KindServer {
		t.Fatalf("Forge fehlt im Katalog oder ist falsch beschrieben: %+v", info)
	}
}

func TestForgeResolvePrefersRecommendedAndHasChecksum(t *testing.T) {
	resolver, server, installer := fakeForge(t)
	ctx := context.Background()
	artifact, err := resolver.Resolve(ctx, Forge, "1.20.1")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.FileName != "forge-1.20.1-47.4.28-installer.jar" || artifact.URL != server.URL+"/forge/1.20.1-47.4.28/forge-1.20.1-47.4.28-installer.jar" {
		t.Fatalf("falscher Installer: %+v", artifact)
	}
	if artifact.SHA256 == "" {
		t.Fatal("der Forge-Installer muss eine Prüfsumme haben")
	}
	dst := filepath.Join(t.TempDir(), "server.jar")
	if err := resolver.Download(ctx, artifact, dst); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(dst); string(data) != string(installer) {
		t.Fatal("Installer falsch heruntergeladen")
	}

	// ohne empfohlene Version: die neueste laut Promotions, Prüfsumme mit Dateiname dahinter
	latest, err := resolver.Resolve(ctx, Forge, "1.21.1")
	if err != nil || latest.FileName != "forge-1.21.1-52.1.21-installer.jar" || latest.SHA256 != strings.Repeat("ab", 32) {
		t.Fatalf("1.21.1: %+v, %v", latest, err)
	}
}

func TestForgeResolveExactAndErrors(t *testing.T) {
	resolver, _, _ := fakeForge(t)
	ctx := context.Background()
	if _, err := resolver.Resolve(ctx, Forge, "1.20.1-47.9.9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unbekannter Build: %v, erwartet ErrNotFound", err)
	}
	if _, err := resolver.Resolve(ctx, Forge, "1.5.2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("zu alte Version: %v, erwartet ErrNotFound", err)
	}
	if _, err := resolver.Resolve(ctx, Forge, "1.20.1-47.4.0"); err == nil || !strings.Contains(err.Error(), "Prüfsumme") {
		t.Fatalf("ungültige Prüfsumme muss abgelehnt werden: %v", err)
	}
	if _, err := resolver.Resolve(ctx, Forge, "1.20.1-47.4.29"); err == nil {
		t.Fatal("ohne Prüfsummendatei darf es keinen Download geben")
	}
}

func TestForgeToMinecraftAndJava(t *testing.T) {
	for input, want := range map[string]string{"1.20.1-47.4.0": "1.20.1", "1.20.1": "1.20.1", "26.3-66.1.9": "26.3"} {
		if got := ForgeToMinecraft(input); got != want {
			t.Errorf("ForgeToMinecraft(%q) = %q, erwartet %q", input, got, want)
		}
	}
	resolver, _, _ := fakeForge(t) // Mojang antwortet nicht: die Schätzung greift
	ctx := context.Background()
	for version, want := range map[string]int{"1.20.1-47.4.0": 17, "1.12.2-14.23.5.2860": 8, "26.3-66.1.9": 25, "1.21.1": 21} {
		if got := resolver.JavaMajor(ctx, Forge, version); got != want {
			t.Errorf("Java für Forge %s = %d, erwartet %d", version, got, want)
		}
	}
}

func TestForgeStartupInstallsOnce(t *testing.T) {
	info, _ := Lookup(Forge)
	for _, part := range []string{"--installServer", "run.sh", "user_jvm_args.txt", "cksum", "{{MEMORY}}", "{{JAR}}"} {
		if !strings.Contains(info.Startup, part) {
			t.Errorf("Startzeile von Forge enthält %q nicht: %s", part, info.Startup)
		}
	}
}
