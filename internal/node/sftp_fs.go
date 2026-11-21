package node

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"

	"github.com/pkg/sftp"
)

// sftpHandler setzt SFTP-Anfragen auf den Ordner eines Servers um. Alles läuft über dessen os.Root (FS), damit ".."
// und Symlinks nie aus dem Ordner hinausführen; vor jeder Operation wird der Zugang neu geprüft.
type sftpHandler struct {
	service  *SFTPService
	serverID string
	username string
	maxWrite int64
}

// root liefert den Ordner des Servers, wenn der Zugang (für write: mit Schreibrecht) noch besteht.
func (h *sftpHandler) root(write bool) (*os.Root, error) {
	server, err := h.service.allowed(h.serverID, h.username, write)
	if err != nil {
		return nil, err
	}
	return server.FS().root, nil
}

// sftpPath macht aus dem SFTP-Pfad (absolut, "/" ist der Serverordner) einen Pfad relativ zum Ordner.
func sftpPath(name string) (string, error) {
	rel, err := clean(name)
	if err != nil {
		return "", sftp.ErrSSHFxBadMessage
	}
	return rel, nil
}

// sftpError übersetzt Fehler in SFTP-Codes, ohne Pfade der Maschine an den Client zu geben.
func sftpError(err error) error {
	err = translate(err)
	var sizeErr *sftpSizeError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &sizeErr):
		return err
	case errors.Is(err, sftp.ErrSSHFxPermissionDenied), errors.Is(err, sftp.ErrSSHFxOpUnsupported), errors.Is(err, sftp.ErrSSHFxBadMessage):
		return err
	case errors.Is(err, fs.ErrNotExist):
		return sftp.ErrSSHFxNoSuchFile
	case errors.Is(err, ErrOutside), errors.Is(err, fs.ErrPermission):
		return sftp.ErrSSHFxPermissionDenied
	case errors.Is(err, fs.ErrExist):
		return errors.New("das Ziel gibt es schon")
	}
	return sftp.ErrSSHFxFailure
}

// Fileread öffnet eine Datei zum Lesen.
func (h *sftpHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	root, err := h.root(false)
	if err != nil {
		return nil, err
	}
	rel, err := sftpPath(r.Filepath)
	if err != nil {
		return nil, err
	}
	file, err := root.Open(rel)
	if err != nil {
		return nil, sftpError(err)
	}
	if info, err := file.Stat(); err != nil || info.IsDir() {
		_ = file.Close()
		return nil, sftp.ErrSSHFxFailure
	}
	return file, nil
}

// Filewrite öffnet eine Datei zum Schreiben (Upload).
func (h *sftpHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	return h.open(r)
}

// OpenFile öffnet eine Datei zum Lesen und Schreiben.
func (h *sftpHandler) OpenFile(r *sftp.Request) (sftp.WriterAtReaderAt, error) {
	return h.open(r)
}

func (h *sftpHandler) open(r *sftp.Request) (*limitedFile, error) {
	root, err := h.root(true)
	if err != nil {
		return nil, err
	}
	rel, err := sftpPath(r.Filepath)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, sftp.ErrSSHFxPermissionDenied
	}
	flags := r.Pflags()
	mode := os.O_RDWR
	if flags.Creat {
		mode |= os.O_CREATE
	}
	if flags.Trunc {
		mode |= os.O_TRUNC
	}
	if flags.Excl {
		mode |= os.O_EXCL
	}
	// O_APPEND passt nicht zu WriteAt; Programme schicken beim Anhängen ohnehin die richtige Position.
	file, err := root.OpenFile(rel, mode, 0o644)
	if err != nil {
		return nil, sftpError(err)
	}
	if info, err := file.Stat(); err != nil || info.IsDir() {
		_ = file.Close()
		return nil, sftp.ErrSSHFxFailure
	}
	return &limitedFile{File: file, max: h.maxWrite}, nil
}

// sftpSizeError meldet eine Datei über der Upload-Grenze.
type sftpSizeError struct{ max int64 }

func (e *sftpSizeError) Error() string {
	return fmt.Sprintf("die Datei ist zu groß (erlaubt sind %d MB)", e.max>>20)
}

// limitedFile lässt nicht über die Upload-Grenze des Nodes hinaus schreiben.
type limitedFile struct {
	*os.File
	max int64
}

func (f *limitedFile) WriteAt(p []byte, offset int64) (int, error) {
	if offset < 0 || offset+int64(len(p)) > f.max {
		return 0, &sftpSizeError{max: f.max}
	}
	return f.File.WriteAt(p, offset)
}

// Filecmd führt verändernde Befehle aus.
func (h *sftpHandler) Filecmd(r *sftp.Request) error {
	root, err := h.root(true)
	if err != nil {
		return err
	}
	rel, err := sftpPath(r.Filepath)
	if err != nil {
		return err
	}
	switch r.Method {
	case "Mkdir":
		if rel == "." {
			return errors.New("das gibt es schon")
		}
		return sftpError(root.Mkdir(rel, 0o755))
	case "Rmdir":
		if rel == "." {
			return sftp.ErrSSHFxPermissionDenied
		}
		info, err := root.Lstat(rel)
		if err != nil {
			return sftpError(err)
		}
		if !info.IsDir() {
			return errors.New("das ist kein Ordner")
		}
		return sftpError(root.Remove(rel))
	case "Remove":
		if rel == "." {
			return sftp.ErrSSHFxPermissionDenied
		}
		info, err := root.Lstat(rel)
		if err != nil {
			return sftpError(err)
		}
		if info.IsDir() {
			return errors.New("das ist ein Ordner")
		}
		return sftpError(root.Remove(rel))
	case "Rename":
		target, err := sftpPath(r.Target)
		if err != nil {
			return err
		}
		if rel == "." || target == "." {
			return sftp.ErrSSHFxPermissionDenied
		}
		// Wie im Panel: Ein vorhandenes Ziel wird nie überschrieben.
		if _, err := root.Lstat(target); err == nil {
			return errors.New("das Ziel gibt es schon")
		}
		return sftpError(root.Rename(rel, target))
	case "Setstat":
		return h.setstat(root, rel, r)
	case "Symlink", "Link":
		// Verknüpfungen könnten im Container woandershin zeigen als hier; deshalb gibt es sie über SFTP nicht.
		return sftp.ErrSSHFxOpUnsupported
	}
	return sftp.ErrSSHFxOpUnsupported
}

func (h *sftpHandler) setstat(root *os.Root, rel string, r *sftp.Request) error {
	flags, attrs := r.AttrFlags(), r.Attributes()
	if attrs == nil {
		return sftp.ErrSSHFxBadMessage
	}
	if flags.Size {
		if int64(attrs.Size) > h.maxWrite {
			return &sftpSizeError{max: h.maxWrite}
		}
		file, err := root.OpenFile(rel, os.O_WRONLY, 0)
		if err != nil {
			return sftpError(err)
		}
		err = file.Truncate(int64(attrs.Size))
		_ = file.Close()
		if err != nil {
			return sftpError(err)
		}
	}
	if flags.Permissions {
		// Nur die üblichen Rechte, nie setuid/setgid/sticky
		if err := root.Chmod(rel, fs.FileMode(attrs.Mode)&0o777); err != nil {
			return sftpError(err)
		}
	}
	if flags.Acmodtime {
		if err := root.Chtimes(rel, attrs.AccessTime(), attrs.ModTime()); err != nil {
			return sftpError(err)
		}
	}
	// Besitzer (UidGid) bleibt, wie er ist: Die Dateien gehören dem Benutzer des Servers.
	return nil
}

// PosixRename benennt um und darf dabei ein vorhandenes Ziel ersetzen (so verlangt es die Erweiterung).
func (h *sftpHandler) PosixRename(r *sftp.Request) error {
	root, err := h.root(true)
	if err != nil {
		return err
	}
	source, err := sftpPath(r.Filepath)
	if err != nil {
		return err
	}
	target, err := sftpPath(r.Target)
	if err != nil {
		return err
	}
	if source == "." || target == "." {
		return sftp.ErrSSHFxPermissionDenied
	}
	return sftpError(root.Rename(source, target))
}

// Filelist beantwortet List und Stat.
func (h *sftpHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	root, err := h.root(false)
	if err != nil {
		return nil, err
	}
	rel, err := sftpPath(r.Filepath)
	if err != nil {
		return nil, err
	}
	switch r.Method {
	case "List":
		dir, err := root.Open(rel)
		if err != nil {
			return nil, sftpError(err)
		}
		defer dir.Close()
		entries, err := dir.ReadDir(-1)
		if err != nil {
			return nil, sftpError(err)
		}
		infos := make([]os.FileInfo, 0, len(entries))
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil {
				infos = append(infos, info)
			}
		}
		return listerAt(infos), nil
	case "Stat":
		info, err := root.Stat(rel)
		if err != nil {
			return nil, sftpError(err)
		}
		return listerAt{renamed{info, baseName(rel)}}, nil
	}
	return nil, sftp.ErrSSHFxOpUnsupported
}

// Lstat liefert die Angaben ohne einem Symlink zu folgen.
func (h *sftpHandler) Lstat(r *sftp.Request) (sftp.ListerAt, error) {
	root, err := h.root(false)
	if err != nil {
		return nil, err
	}
	rel, err := sftpPath(r.Filepath)
	if err != nil {
		return nil, err
	}
	info, err := root.Lstat(rel)
	if err != nil {
		return nil, sftpError(err)
	}
	return listerAt{renamed{info, baseName(rel)}}, nil
}

// Readlink liefert das Ziel eines Symlinks (nur als Text; folgen kann man ihm nur innerhalb des Ordners).
func (h *sftpHandler) Readlink(name string) (string, error) {
	root, err := h.root(false)
	if err != nil {
		return "", err
	}
	rel, err := sftpPath(name)
	if err != nil {
		return "", err
	}
	target, err := root.Readlink(rel)
	if err != nil {
		return "", sftpError(err)
	}
	return target, nil
}

// renamed ersetzt den Namen (os.Root liefert für "." sonst einen Punkt statt "/").
type renamed struct {
	os.FileInfo
	name string
}

func (r renamed) Name() string { return r.name }

func baseName(rel string) string {
	if rel == "." {
		return "/"
	}
	return path.Base(rel)
}

type listerAt []os.FileInfo

func (l listerAt) ListAt(into []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(into, l[offset:])
	if n < len(into) {
		return n, io.EOF
	}
	return n, nil
}
