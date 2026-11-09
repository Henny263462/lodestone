package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// DockerRuntime setzt Runtime mit der Docker Engine API um.
type DockerRuntime struct {
	cli *client.Client
}

// NewDockerRuntime verbindet mit Docker (DOCKER_HOST oder dem Standard der Plattform). Es wird noch nicht geprüft,
// ob der Dienst läuft; das tut Info.
func NewDockerRuntime(host string) (*DockerRuntime, error) {
	options := []client.Opt{client.FromEnv}
	if host == "" && os.Getenv("DOCKER_HOST") == "" {
		host = defaultHost()
	}
	if host != "" {
		options = append(options, client.WithHost(host))
	}
	cli, err := client.New(options...)
	if err != nil {
		return nil, err
	}
	return &DockerRuntime{cli: cli}, nil
}

// defaultHost wählt unter Windows die Pipe, die es wirklich gibt: Docker Desktop nutzt je nach Version
// dockerDesktopLinuxEngine (wie das docker-Programm) oder die ältere docker_engine. Sonst gilt der Standard des SDK.
func defaultHost() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	for _, name := range []string{"docker_engine", "dockerDesktopLinuxEngine"} {
		if _, err := os.Stat(`\\.\pipe\` + name); err == nil {
			return "npipe:////./pipe/" + name
		}
	}
	return ""
}

func (d *DockerRuntime) Info(ctx context.Context) (SystemInfo, error) {
	result, err := d.cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return SystemInfo{}, err
	}
	info := result.Info
	return SystemInfo{DockerVersion: info.ServerVersion, OS: info.OperatingSystem, Architecture: info.Architecture, CPUs: info.NCPU, MemoryBytes: info.MemTotal}, nil
}

func (d *DockerRuntime) EnsureNetwork(ctx context.Context, name string) error {
	if name == "" {
		return nil
	}
	if _, err := d.cli.NetworkInspect(ctx, name, client.NetworkInspectOptions{}); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return err
	}
	_, err := d.cli.NetworkCreate(ctx, name, client.NetworkCreateOptions{Driver: "bridge", Labels: map[string]string{LabelServer + ".network": "true"}})
	if err != nil && cerrdefs.IsConflict(err) {
		return nil // ein anderer Aufruf war schneller
	}
	return err
}

func (d *DockerRuntime) EnsureImage(ctx context.Context, image string) error {
	if _, err := d.cli.ImageInspect(ctx, image); err == nil {
		return nil
	} else if !cerrdefs.IsNotFound(err) {
		return err
	}
	response, err := d.cli.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("Image %s konnte nicht geladen werden: %w", image, err)
	}
	defer response.Close()
	return response.Wait(ctx)
}

func (d *DockerRuntime) Create(ctx context.Context, spec ContainerSpec) (string, error) {
	exposed := network.PortSet{}
	bindings := network.PortMap{}
	for _, mapping := range spec.Ports {
		protocol := network.TCP
		if mapping.Protocol == "udp" {
			protocol = network.UDP
		}
		port, ok := network.PortFrom(uint16(mapping.Container), protocol)
		if !ok {
			return "", fmt.Errorf("ungültiger Port %d", mapping.Container)
		}
		exposed[port] = struct{}{}
		bindings[port] = []network.PortBinding{{HostPort: fmt.Sprint(mapping.Host)}}
	}
	pids := int64(4096)
	init := true
	result, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: spec.Name,
		Config: &container.Config{
			Image:        spec.Image,
			Cmd:          spec.Cmd,
			Env:          spec.Env,
			WorkingDir:   spec.WorkDir,
			User:         spec.User,
			Labels:       spec.Labels,
			ExposedPorts: exposed,
			Tty:          true,
			OpenStdin:    true,
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
		},
		HostConfig: &container.HostConfig{
			NetworkMode:   container.NetworkMode(spec.Network),
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyMode(spec.Restart)},
			Binds:         spec.Binds,
			PortBindings:  bindings,
			Init:          &init,
			// Ein Minecraft-Server braucht kaum Rechte: alles weg, nur das Nötigste zurück.
			CapDrop:     []string{"ALL"},
			CapAdd:      []string{"CHOWN", "DAC_OVERRIDE", "FOWNER", "SETGID", "SETUID", "NET_BIND_SERVICE"},
			SecurityOpt: []string{"no-new-privileges"},
			Resources: container.Resources{
				Memory:     spec.MemoryBytes,
				MemorySwap: spec.MemoryBytes, // kein Swap
				NanoCPUs:   spec.NanoCPUs,
				PidsLimit:  &pids,
			},
		},
	})
	if err != nil {
		return "", err
	}
	return result.ID, nil
}

// Logs liest die letzten Zeilen (der Container hat ein TTY, die Ausgabe kommt deshalb ohne Kopfzeilen).
func (d *DockerRuntime) Logs(ctx context.Context, id string, lines int) (string, error) {
	result, err := d.cli.ContainerLogs(ctx, id, client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Tail: fmt.Sprint(lines)})
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			return "", nil
		}
		return "", err
	}
	defer result.Close()
	data, err := io.ReadAll(io.LimitReader(result, 256<<10))
	return string(data), err
}

func (d *DockerRuntime) Start(ctx context.Context, id string) error {
	_, err := d.cli.ContainerStart(ctx, id, client.ContainerStartOptions{})
	return err
}

func (d *DockerRuntime) Stop(ctx context.Context, id string, timeout time.Duration) error {
	seconds := int(timeout.Seconds())
	_, err := d.cli.ContainerStop(ctx, id, client.ContainerStopOptions{Timeout: &seconds})
	return ignoreGone(err)
}

func (d *DockerRuntime) Kill(ctx context.Context, id string) error {
	_, err := d.cli.ContainerKill(ctx, id, client.ContainerKillOptions{})
	return ignoreGone(err)
}

func (d *DockerRuntime) Remove(ctx context.Context, id string) error {
	_, err := d.cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
	return ignoreGone(err)
}

// ignoreGone behandelt "gibt es nicht" und "läuft nicht" als Erfolg: Das Ziel ist ohnehin erreicht.
func ignoreGone(err error) error {
	if err == nil || cerrdefs.IsNotFound(err) || cerrdefs.IsConflict(err) || cerrdefs.IsNotModified(err) {
		return nil
	}
	return err
}

func (d *DockerRuntime) Inspect(ctx context.Context, id string) (ContainerState, error) {
	result, err := d.cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return ContainerState{}, nil
	}
	if err != nil {
		return ContainerState{}, err
	}
	state := result.Container.State
	if state == nil {
		return ContainerState{Exists: true}, nil
	}
	return ContainerState{Exists: true, Running: state.Running, ExitCode: state.ExitCode}, nil
}

type attached struct {
	reader io.Reader
	closer func()
	writer io.Writer
}

func (a *attached) Read(p []byte) (int, error)  { return a.reader.Read(p) }
func (a *attached) Write(p []byte) (int, error) { return a.writer.Write(p) }
func (a *attached) Close() error                { a.closer(); return nil }

func (d *DockerRuntime) Attach(ctx context.Context, id string, replay bool) (io.ReadWriteCloser, error) {
	result, err := d.cli.ContainerAttach(ctx, id, client.ContainerAttachOptions{Stream: true, Stdin: true, Stdout: true, Stderr: true, Logs: replay})
	if err != nil {
		return nil, err
	}
	response := result.HijackedResponse
	return &attached{reader: response.Reader, writer: response.Conn, closer: response.Close}, nil
}

func (d *DockerRuntime) Wait(ctx context.Context, id string) (int, error) {
	wait := d.cli.ContainerWait(ctx, id, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case result := <-wait.Result:
		return int(result.StatusCode), nil
	case err := <-wait.Error:
		if cerrdefs.IsNotFound(err) {
			return 0, nil
		}
		return 0, err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (d *DockerRuntime) StatsStream(ctx context.Context, id string, report func(Stats)) error {
	result, err := d.cli.ContainerStats(ctx, id, client.ContainerStatsOptions{Stream: true})
	if err != nil {
		return err
	}
	defer result.Body.Close()
	decoder := json.NewDecoder(result.Body)
	var previous container.StatsResponse
	first := true
	for {
		var sample container.StatsResponse
		if err := decoder.Decode(&sample); err != nil {
			if errors.Is(err, io.EOF) || ctx.Err() != nil {
				return nil
			}
			return err
		}
		stats := Stats{MemoryBytes: memoryUsage(sample), MemoryLimit: sample.MemoryStats.Limit}
		for _, net := range sample.Networks {
			stats.NetRx += net.RxBytes
			stats.NetTx += net.TxBytes
		}
		if !first {
			cpuDelta := float64(sample.CPUStats.CPUUsage.TotalUsage) - float64(previous.CPUStats.CPUUsage.TotalUsage)
			systemDelta := float64(sample.CPUStats.SystemUsage) - float64(previous.CPUStats.SystemUsage)
			cpus := float64(sample.CPUStats.OnlineCPUs)
			if cpus == 0 {
				cpus = float64(len(sample.CPUStats.CPUUsage.PercpuUsage))
			}
			if systemDelta > 0 && cpuDelta > 0 && cpus > 0 {
				stats.CPUPercent = cpuDelta / systemDelta * cpus * 100
			}
		}
		first = false
		previous = sample
		report(stats)
	}
}

// memoryUsage rechnet den Zwischenspeicher heraus, wie es "docker stats" auch tut.
func memoryUsage(sample container.StatsResponse) uint64 {
	usage := sample.MemoryStats.Usage
	for _, key := range []string{"inactive_file", "total_inactive_file", "cache"} {
		if cache, ok := sample.MemoryStats.Stats[key]; ok && cache < usage {
			return usage - cache
		}
	}
	return usage
}

func (d *DockerRuntime) List(ctx context.Context) ([]ContainerRef, error) {
	result, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true, Filters: make(client.Filters).Add("label", LabelServer)})
	if err != nil {
		return nil, err
	}
	refs := make([]ContainerRef, 0, len(result.Items))
	for _, item := range result.Items {
		refs = append(refs, ContainerRef{ID: item.ID, ServerID: item.Labels[LabelServer], Running: string(item.State) == "running"})
	}
	return refs, nil
}

// PullImage zieht das Image immer neu von der Registry, auch wenn eine ältere Version mit demselben Tag lokal liegt.
func (d *DockerRuntime) PullImage(ctx context.Context, image string) error {
	response, err := d.cli.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("Image %s konnte nicht geladen werden: %w", image, err)
	}
	defer response.Close()
	return response.Wait(ctx)
}

// Namen der Lodestone-Images. Nur Container mit so einem Image fasst der Updater an, nie fremde Container,
// die zufällig „panel“ oder „node“ heißen.
const (
	panelImageName = "lodestone-panel"
	nodeImageName  = "lodestone-node"
	// Namensanfang der Hilfscontainer, die einen Node-Daemon ersetzen; sie haben dasselbe Image wie der Daemon.
	updaterContainerPrefix = "lodestone-node-updater-"
)

// imageRepo schneidet Tag und Digest von einer Image-Angabe ab („ghcr.io/x/y:1“ wird zu „ghcr.io/x/y“).
func imageRepo(ref string) string {
	ref, _, _ = strings.Cut(ref, "@")
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref
}

// isLodestoneImage meldet, ob ref das Image name bezeichnet (die Registry davor ist egal).
func isLodestoneImage(ref, name string) bool {
	repo := imageRepo(ref)
	return repo == name || strings.HasSuffix(repo, "/"+name)
}

// releaseImage setzt den Tag der Zielversion („v0.3.0“ bei Releases, „dev-1a2b3c4“ bei Dev-Builds) auf die Image-Angabe des
// laufenden Containers. So kommt das neue Image aus derselben Registry wie das alte, und von außen lässt sich kein fremdes
// Image einschleusen.
func releaseImage(current, version string) string {
	if strings.HasPrefix(version, "dev-") {
		return imageRepo(current) + ":" + version
	}
	return imageRepo(current) + ":v" + strings.TrimPrefix(version, "v")
}

// findLodestoneContainer sucht den Container, dessen Image name ist. short ist der Compose-Dienstname und dient nur
// der Vorauswahl; entschieden wird erst an dem Image, das der Container beim Anlegen hatte.
func (d *DockerRuntime) findLodestoneContainer(ctx context.Context, name, short string) (id, image string, err error) {
	result, err := d.cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return "", "", err
	}
	for _, item := range result.Items {
		if hasContainerNamePrefix(item.Names, updaterContainerPrefix) {
			continue // Hilfscontainer eines laufenden oder gescheiterten Updates
		}
		if !strings.Contains(item.Image, name) && item.Labels["com.docker.compose.service"] != short && !hasContainerName(item.Names, short) {
			continue
		}
		inspect, err := d.cli.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
		if err != nil || inspect.Container.Config == nil {
			continue
		}
		if isLodestoneImage(inspect.Container.Config.Image, name) {
			return item.ID, inspect.Container.Config.Image, nil
		}
	}
	return "", "", fmt.Errorf("kein %s-Container auf dieser Maschine gefunden", name)
}

// UpdatePanel zieht das Image des Releases version und ersetzt damit den Panel-Container dieser Maschine. Der Austausch
// läuft erst nach der Antwort, weil das Panel dabei seine eigene Anfrage verliert.
func (d *DockerRuntime) UpdatePanel(ctx context.Context, version string) error {
	targetID, current, err := d.findLodestoneContainer(ctx, panelImageName, "panel")
	if err != nil {
		return err
	}
	image := releaseImage(current, version)
	if err := d.PullImage(ctx, image); err != nil {
		return err
	}
	go func() {
		time.Sleep(2 * time.Second)
		replaceCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := d.replaceContainer(replaceCtx, targetID, image); err != nil {
			slog.Warn("Panel-Update fehlgeschlagen", "error", err)
		}
	}()
	return nil
}

// UpdateSelf zieht das Image des Releases version und startet einen Hilfscontainer, der den Daemon-Container ersetzt
// (ein Container kann sich nicht selbst austauschen).
func (d *DockerRuntime) UpdateSelf(ctx context.Context, version string) error {
	targetID, current := d.findSelf(ctx)
	if targetID == "" {
		return errors.New("der eigene Node-Container konnte nicht ermittelt werden (läuft der Daemon im Docker-Container?)")
	}
	image := releaseImage(current, version)
	if err := d.PullImage(ctx, image); err != nil {
		return err
	}

	helper, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: fmt.Sprintf("%s%x", updaterContainerPrefix, time.Now().UnixNano()&0xffff),
		Config: &container.Config{
			Image: image,
			// Der Entrypoint des Images ist schon „lodestone-node“, hier stehen nur die Flags.
			Cmd: []string{"-update-self=" + targetID, "-update-image=" + image},
		},
		HostConfig: &container.HostConfig{
			Binds:      []string{"/var/run/docker.sock:/var/run/docker.sock"},
			AutoRemove: true,
		},
	})
	if err != nil {
		return fmt.Errorf("Update-Hilfscontainer konnte nicht angelegt werden: %w", err)
	}
	if _, err := d.cli.ContainerStart(ctx, helper.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = d.cli.ContainerRemove(context.WithoutCancel(ctx), helper.ID, client.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("Update-Hilfscontainer konnte nicht gestartet werden: %w", err)
	}
	return nil
}

// findSelf liefert ID und Image-Angabe des Containers, in dem dieser Daemon läuft (leer, wenn er nicht in Docker läuft).
func (d *DockerRuntime) findSelf(ctx context.Context) (id, image string) {
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		if inspect, err := d.cli.ContainerInspect(ctx, hostname, client.ContainerInspectOptions{}); err == nil &&
			inspect.Container.Config != nil && isLodestoneImage(inspect.Container.Config.Image, nodeImageName) {
			return inspect.Container.ID, inspect.Container.Config.Image
		}
	}
	id, image, _ = d.findLodestoneContainer(ctx, nodeImageName, "node")
	return id, image
}

// replaceContainer ersetzt den Container targetID durch einen neuen mit derselben Konfiguration und dem Image newImage.
// Der alte bleibt unter anderem Namen erhalten, bis der neue läuft; scheitert etwas, holt die Funktion ihn zurück.
func (d *DockerRuntime) replaceContainer(ctx context.Context, targetID, newImage string) error {
	inspectRes, err := d.cli.ContainerInspect(ctx, targetID, client.ContainerInspectOptions{})
	if err != nil {
		return fmt.Errorf("Container %s konnte nicht inspiziert werden: %w", targetID, err)
	}
	inspect := inspectRes.Container
	if inspect.Config == nil || inspect.HostConfig == nil {
		return errors.New("Container hat keine Konfiguration")
	}
	name := strings.TrimPrefix(inspect.Name, "/")
	cfg := *inspect.Config
	cfg.Image = newImage
	// Der Hostname ist die Kurz-ID des alten Containers; der neue soll seine eigene bekommen (findSelf sucht danach).
	if cfg.Hostname != "" && strings.HasPrefix(targetID, cfg.Hostname) {
		cfg.Hostname = ""
	}
	var netCfg *network.NetworkingConfig
	if inspect.NetworkSettings != nil && len(inspect.NetworkSettings.Networks) > 0 {
		netCfg = &network.NetworkingConfig{EndpointsConfig: inspect.NetworkSettings.Networks}
	}

	oldName := fmt.Sprintf("%s-alt-%d", name, time.Now().Unix())
	if _, err := d.cli.ContainerRename(ctx, targetID, client.ContainerRenameOptions{NewName: oldName}); err != nil {
		return fmt.Errorf("Container %s konnte nicht umbenannt werden: %w", name, err)
	}
	restore := func() {
		rollback := context.WithoutCancel(ctx)
		_, _ = d.cli.ContainerRename(rollback, targetID, client.ContainerRenameOptions{NewName: name})
		_, _ = d.cli.ContainerStart(rollback, targetID, client.ContainerStartOptions{})
	}

	created, err := d.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name:             name,
		Config:           &cfg,
		HostConfig:       inspect.HostConfig,
		NetworkingConfig: netCfg,
	})
	if err != nil {
		restore()
		return fmt.Errorf("neuer Container %s konnte nicht angelegt werden: %w", name, err)
	}
	timeout := 15
	_, _ = d.cli.ContainerStop(ctx, targetID, client.ContainerStopOptions{Timeout: &timeout})
	if _, err := d.cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = d.cli.ContainerRemove(context.WithoutCancel(ctx), created.ID, client.ContainerRemoveOptions{Force: true})
		restore()
		return fmt.Errorf("neuer Container %s konnte nicht gestartet werden: %w", name, err)
	}
	_, _ = d.cli.ContainerRemove(ctx, targetID, client.ContainerRemoveOptions{Force: true})
	return nil
}

// hasContainerNamePrefix meldet, ob einer der Namen mit prefix beginnt.
func hasContainerNamePrefix(names []string, prefix string) bool {
	for _, n := range names {
		if strings.HasPrefix(strings.TrimPrefix(n, "/"), prefix) {
			return true
		}
	}
	return false
}

// hasContainerName meldet, ob einer der Namen match ist oder wie ein Compose-Name („projekt-match-1“) endet.
func hasContainerName(names []string, match string) bool {
	for _, n := range names {
		trimmed := strings.TrimPrefix(n, "/")
		if trimmed == match || strings.HasSuffix(trimmed, "-"+match+"-1") || strings.HasSuffix(trimmed, "_"+match+"_1") {
			return true
		}
	}
	return false
}

// RunSelfUpdate wird vom temporären Update-Container aufgerufen, um den bisherigen Daemon-Container zu ersetzen.
func RunSelfUpdate(targetID, image string) error {
	time.Sleep(2 * time.Second) // Warten, bis die HTTP-Antwort gesendet wurde
	rt, err := NewDockerRuntime("")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return rt.replaceContainer(ctx, targetID, image)
}
