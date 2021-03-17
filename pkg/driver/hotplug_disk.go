package driver

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"
)

const BlikidNotFound int = 2

type DiskHotPlugger interface {
	// Format erases the path with a new empty filesystem
	Format(path, filesystem string) error

	// Mount the path to the mountpoint, specifying the current filesystem and mount flags to use
	Mount(path, mountpoint, filesystem string, flags ...string) error

	// Unmount unmounts the given mountpoint
	Unmount(mountpoint string) error

	// IsFormatted returns true if the device path is already formatted
	IsFormatted(path string) (bool, error)

	// IsMounted returns true if the target has a disk mounted there
	IsMounted(target string) (bool, error)
}

type RealDiskHotPlugger struct{}

// Format erases the path with a new empty filesystem
func (p *RealDiskHotPlugger) Format(path, filesystem string) error {
	log.Debug().Str("path", path).Msgf("Formatting with %s", filesystem)

	output, err := exec.Command(("mkfs." + filesystem), path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Formatting with 'mkfs.%s %s' failed: %v output: %s", filesystem, path, err, string(output))
	}

	return nil
}

// Mount the path to the mountpoint, specifying the current filesystem and mount flags to use
func (p *RealDiskHotPlugger) Mount(path, mountpoint, filesystem string, flags ...string) error {
	args := []string{}

	if filesystem == "" {
		// Bind-mount requires a file to bind to
		log.Debug().Str("path", path).Str("mountpoint", mountpoint).Msg("Bind mounting filesystem, making parent folder")
		err := os.MkdirAll(filepath.Dir(mountpoint), 0750)
		if err != nil {
			return fmt.Errorf("creating mountpoint containing folder failed: %v", err)
		}

		log.Debug().Str("mountpoint", mountpoint).Msg("Making bind-mount file")
		file, err := os.OpenFile(mountpoint, os.O_CREATE, 0660)
		if err != nil {
			return fmt.Errorf("failed to create target file for raw block bind mount: %v", err)
		}
		file.Close()
	} else {
		// Block mounts require a folder to mount to
		log.Debug().Str("mountpoint", mountpoint).Msg("Device mounting - ensuring folder exists")

		err := os.MkdirAll(mountpoint, 0750)
		if err != nil {
			return fmt.Errorf("creating mountpoint failed: %v", err)
		}
		args = append(args, "-t", filesystem)
	}

	if len(flags) > 0 {
		args = append(args, "-o", strings.Join(flags, ","))
	}

	args = append(args, path)
	args = append(args, mountpoint)

	log.Debug().Str("path", path).Str("mountpoint", mountpoint).Msg("Mounting device")

	output, err := exec.Command("mount", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Mounting with 'mount %s' failed: %v output: %s", strings.Join(args, " "), err, string(output))
	}

	return nil
}

// Unmount unmounts the given mountpoint
func (p *RealDiskHotPlugger) Unmount(mountpoint string) error {
	log.Debug().Str("mountpoint", mountpoint).Msg("Unmounting mountpoint")
	output, err := exec.Command("umount", mountpoint).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Unmounting with 'umount %s' failed: %v output: %s", mountpoint, err, string(output))
	}

	return nil
}

// IsFormatted returns true if the device path is already formatted
func (p *RealDiskHotPlugger) IsFormatted(path string) (bool, error) {
	if path == "" {
		return false, errors.New("path is not empty")
	}

	_, err := exec.LookPath("blkid")
	if err != nil {
		if err == exec.ErrNotFound {
			return false, fmt.Errorf("blkid executable not found in $PATH")
		}
		return false, err
	}

	args := []string{path}

	cmd := exec.Command("blkid", args...)
	err = cmd.Run()
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if !ok {
			return false, fmt.Errorf("is device formatted err: %v cmd: blkid %q", err, args)
		}
		exitCode := exitError.Sys().(syscall.WaitStatus).ExitStatus()
		if exitCode == BlikidNotFound {
			return false, nil
		}
		return false, fmt.Errorf("is device formatted err: %v cmd: blkid %q", err, args)
	}

	return true, nil
}

// IsMounted returns true if the target has a disk mounted there
func (p *RealDiskHotPlugger) IsMounted(path string) (bool, error) {
	if path == "" {
		return false, errors.New("path is not empty")
	}

	_, err := exec.LookPath("findmnt")
	if err != nil {
		if err == exec.ErrNotFound {
			return false, fmt.Errorf("findmnt executable not found in $PATH")
		}
		return false, err
	}

	args := []string{"-n", "-T", path}
	cmd := exec.Command("findmnt", args...)
	err = cmd.Run()
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if !ok {
			return false, fmt.Errorf("is device mounted err: %v cmd: findmnt %q", err, args)
		}
		exitCode := exitError.Sys().(syscall.WaitStatus).ExitStatus()
		if exitCode == BlikidNotFound {
			return false, nil
		}
		return false, fmt.Errorf("is device mounted err: %v cmd: findmnt %q", err, args)
	}

	return true, nil
}

type FakeDiskHotPlugger struct {
	Filesystem   string
	Formatted    bool
	FormatCalled bool
	Device       string
	Mountpoint   string
	Mounted      bool
	MountCalled  bool
}

// Format erases the path with a new empty filesystem
func (p *FakeDiskHotPlugger) Format(path, filesystem string) error {
	p.Device = path
	p.Formatted = true
	p.FormatCalled = true
	return nil
}

// Mount the path to the mountpoint, specifying the current filesystem and mount flags to use
func (p *FakeDiskHotPlugger) Mount(path, mountpoint, filesystem string, flags ...string) error {
	p.Device = path
	p.Mountpoint = mountpoint
	p.Mounted = true
	p.MountCalled = true
	return nil
}

// Unmount unmounts the given mountpoint
func (p *FakeDiskHotPlugger) Unmount(mountpoint string) error {
	p.Mountpoint = ""
	p.Mounted = false
	return nil
}

// IsFormatted returns true if the device path is already formatted
func (p *FakeDiskHotPlugger) IsFormatted(path string) (bool, error) {
	return p.Formatted, nil
}

// IsMounted returns true if the target has a disk mounted there
func (p *FakeDiskHotPlugger) IsMounted(target string) (bool, error) {
	return p.Mounted, nil
}
