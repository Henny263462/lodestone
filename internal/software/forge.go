package software

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// forgeMinimum ist die älteste Minecraft-Version, für die Lodestone Forge anbietet. Ältere Installer brauchen eigene
// Startwege (Launchwrapper, Java 8 mit alten Zertifikaten), die sich nicht zuverlässig im Container betreiben lassen.
const forgeMinimum = "1.12"

// forgeStartup installiert Forge beim ersten Start (und nach jedem Wechsel des Installers, erkannt an der Prüfsumme) und
// startet den Server. Ab Minecraft 1.17 legt der Installer ein run.sh an, ältere Versionen eine forge-*.jar.
const forgeStartup = "if [ \"$(cat .lodestone-forge 2>/dev/null)\" != \"$(cksum < {{JAR}})\" ]; then rm -f run.sh; java -jar {{JAR}} --installServer || exit 1; cksum < {{JAR}} > .lodestone-forge; fi; " +
	"if [ -f run.sh ]; then sed -i 's/^java/exec java/' run.sh 2>/dev/null || true; echo '" + jvmFlags + "' > user_jvm_args.txt; exec sh run.sh nogui; fi; " +
	"exec java " + jvmFlags + " -jar \"$(ls -t forge-*.jar | grep -v installer | head -n 1)\" nogui"

const jvmFlags = "-Xms{{MEMORY}}M -Xmx{{MEMORY}}M -XX:+UseG1GC -XX:+ParallelRefProcEnabled -XX:MaxGCPauseMillis=200 -XX:+DisableExplicitGC -Dterminal.jline=false -Dterminal.ansi=true"

// ForgeToMinecraft liefert zu einer Forge-Version wie 1.20.1-47.4.0 die Minecraft-Version (1.20.1). Ist version schon
// eine Minecraft-Version, bleibt sie unverändert.
func ForgeToMinecraft(version string) string {
	if cut := strings.Index(version, "-"); cut > 0 {
		return version[:cut]
	}
	return version
}

type forgeMetadata struct {
	Versioning struct {
		Versions []string `xml:"versions>version"`
	} `xml:"versioning"`
}

type forgePromotions struct {
	Promos map[string]string `json:"promos"`
}

// forgeVersions nennt die Minecraft-Versionen, für die es Forge gibt (ab forgeMinimum), die neueste zuerst.
func (r *Resolver) forgeVersions(ctx context.Context, prerelease bool) ([]string, error) {
	var meta forgeMetadata
	if err := r.getXML(ctx, r.ForgeMaven+"/maven-metadata.xml", &meta); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	versions := []string{}
	for _, full := range meta.Versioning.Versions {
		mc := ForgeToMinecraft(full)
		if mc == full || seen[mc] || CompareVersions(mc, forgeMinimum) < 0 || (!prerelease && isPrerelease(mc)) {
			continue
		}
		seen[mc] = true
		versions = append(versions, mc)
	}
	sort.Slice(versions, func(i, j int) bool { return CompareVersions(versions[i], versions[j]) > 0 })
	return versions, nil
}

// forgeTarget wählt die Forge-Version: eine genaue Angabe (1.20.1-47.4.0) muss es geben; zu einer Minecraft-Version
// nimmt sie die empfohlene, sonst die neueste laut Promotions, sonst die höchste aus den Metadaten.
func (r *Resolver) forgeTarget(ctx context.Context, version string) (string, error) {
	var meta forgeMetadata
	if err := r.getXML(ctx, r.ForgeMaven+"/maven-metadata.xml", &meta); err != nil {
		return "", err
	}
	known := map[string]bool{}
	for _, full := range meta.Versioning.Versions {
		known[full] = true
	}
	if strings.Contains(version, "-") {
		if known[version] {
			return version, nil
		}
		return "", ErrNotFound
	}
	if CompareVersions(version, forgeMinimum) < 0 {
		return "", ErrNotFound
	}
	var promotions forgePromotions
	if err := r.getJSON(ctx, r.ForgePromotions, &promotions); err == nil {
		for _, kind := range []string{"recommended", "latest"} {
			if build := promotions.Promos[version+"-"+kind]; build != "" && known[version+"-"+build] {
				return version + "-" + build, nil
			}
		}
	}
	best := ""
	for full := range known {
		if ForgeToMinecraft(full) == version && (best == "" || CompareVersions(forgeBuild(full), forgeBuild(best)) > 0) {
			best = full
		}
	}
	if best == "" {
		return "", ErrNotFound
	}
	return best, nil
}

// forgeBuild ist der Teil nach der Minecraft-Version (47.4.0), damit sich Builds zahlenweise vergleichen lassen.
func forgeBuild(full string) string {
	if cut := strings.Index(full, "-"); cut > 0 {
		return full[cut+1:]
	}
	return full
}

// resolveForge liefert den Installer samt SHA-256 aus dem offiziellen Maven von Forge.
func (r *Resolver) resolveForge(ctx context.Context, version string) (Artifact, error) {
	target, err := r.forgeTarget(ctx, version)
	if err != nil {
		return Artifact{}, err
	}
	name := "forge-" + target + "-installer.jar"
	address := fmt.Sprintf("%s/%s/%s", r.ForgeMaven, url.PathEscape(target), url.PathEscape(name))
	sum, err := r.getText(ctx, address+".sha256")
	if err != nil {
		return Artifact{}, fmt.Errorf("die Prüfsumme des Forge-Installers ist nicht abrufbar: %w", err)
	}
	fields := strings.Fields(sum)
	if len(fields) > 0 {
		sum = strings.ToLower(fields[0])
	}
	if len(fields) == 0 || len(sum) != 64 || strings.Trim(sum, "0123456789abcdef") != "" {
		return Artifact{}, fmt.Errorf("Forge meldet eine ungültige Prüfsumme für %s", target)
	}
	return Artifact{URL: address, FileName: name, SHA256: sum}, nil
}

func (r *Resolver) getText(ctx context.Context, address string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Lodestone")
	response, err := r.HTTP.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", ErrNotFound
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s antwortet mit Status %d", request.URL.Host, response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	return string(data), err
}
