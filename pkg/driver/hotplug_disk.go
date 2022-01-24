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
	// PathForVolume returns the path of the hotplugged disk
	PathForVolume(volumeID string) string

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

// PathForVolume returns the path of the hotplugged disk
func (p *RealDiskHotPlugger) PathForVolume(volumeID string) string {
	matches, _ := filepath.Glob(fmt.Sprintf("/dev/disk/by-id/*%s", volumeID))
	if len(matches) >= 1 {
		return matches[0]
	}

	return ""
}

// Format erases the path with a new empty filesystem
func (p *RealDiskHotPlugger) Format(path, filesystem string) error {
	log.Debug().Str("path", path).Str("filesystem", filesystem).Msg("Formatting")

	output, err := exec.Command(("mkfs." + filesystem), path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("Formatting with 'mkfs.%s %s' failed: %v output: %s", filesystem, path, err, string(output))
	}

	formatted, err := p.IsFormatted(path)
	if err != nil {
		return err
	}
	if !formatted {
		return fmt.Errorf("Failed to ensure it was formatted, output of 'mkfs.%s %s' is %s", filesystem, path, string(output))
	}

	return nil
}

// Mount the path to the mountpoint, specifying the current filesystem and mount flags to use
func (p *RealDiskHotPlugger) Mount(path, mountpoint, filesystem string, flags ...string) error {
	log.Debug().Str("path", path).Str("filesystem", filesystem).Str("mountpoint", mountpoint).Msg("Mounting")
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

	mounted, err := p.IsMounted(mountpoint)
	if err != nil {
		return err
	}
	if !mounted {
		return fmt.Errorf("After apparently successful mounting, still not mounted - mount %s output: %s", strings.Join(args, " "), string(output))
	}

	log.Debug().Str("path", path).Str("filesystem", filesystem).Str("mountpoint", mountpoint).Msg("Mounting succeeded")

	return nil
}

// Unmount unmounts the given mountpoint
func (p *RealDiskHotPlugger) Unmount(mountpoint string) error {
	log.Debug().Str("mountpoint", mountpoint).Msg("Unmounting mountpoint")
	output, err := exec.Command("umount", mountpoint).CombinedOutput()
	log.Debug().Str("output", string(output)).Msg("Unmounting command output")
	if err != nil {
		return fmt.Errorf("Unmounting with 'umount %s' failed: %v output: %s", mountpoint, err, string(output))
	}

	return nil
}

// IsFormatted returns true if the device path is already formatted
func (p *RealDiskHotPlugger) IsFormatted(path string) (bool, error) {
	log.Debug().Str("path", path).Msg("Checking if path is formatted")
	if path == "" {
		return false, errors.New("path to check is empty")
	}

	_, err := exec.LookPath("blkid")
	if err != nil {
		if err == exec.ErrNotFound {
			log.Error().Msg("Could not find 'blkid' in $PATH")
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
			log.Error().Err(err).Msg("Unable to determine if device is formatted")
			return false, fmt.Errorf("is device formatted err: %v cmd: blkid %q", err, args)
		}

		exitCode := exitError.Sys().(syscall.WaitStatus).ExitStatus()
		if exitCode == BlikidNotFound {
			log.Debug().Str("path", path).Msg("Path is not formatted")
			return false, nil
		}

		log.Error().Err(err).Msg("Unable to determine if device is formatted")
		return false, fmt.Errorf("is device formatted err: %v cmd: blkid %q", err, args)
	}

	log.Debug().Str("path", path).Msg("Path is formatted")
	return true, nil
}

// IsMounted returns true if the target has a disk mounted there
func (p *RealDiskHotPlugger) IsMounted(path string) (bool, error) {
	log.Debug().Str("path", path).Msg("Checking if path is mounted")
	if path == "" {
		return false, errors.New("path is empty")
	}

	_, err := exec.LookPath("findmnt")
	if err != nil {
		if err == exec.ErrNotFound {
			log.Error().Msg("Could not find 'findmnt' in $PATH")
			return false, fmt.Errorf("findmnt executable not found in $PATH")
		}
		return false, err
	}

	args := []string{"-M", path}
	out, err := exec.Command("findmnt", args...).CombinedOutput()
	if err != nil {
		if strings.TrimSpace(string(out)) == "" {
			log.Debug().Str("path", path).Msg("Path is not mounted")
			return false, nil
		}

		log.Error().Err(err).Msg("Unable to determine if device is mounted")
		return false, fmt.Errorf("is device mounted err: %v cmd: findmnt %q", err, args)
	}

	log.Debug().Str("path", path).Msg("Path is mounted")

	return true, nil
}

type FakeDiskHotPlugger struct {
	DiskAttachmentMissing bool
	Filesystem            string
	Formatted             bool
	FormatCalled          bool
	Device                string
	Mountpoint            string
	Mounted               bool
	MountCalled           bool
}

// PathForVolume returns the path of the hotplugged disk
func (p *FakeDiskHotPlugger) PathForVolume(volumeID string) string {
	if p.DiskAttachmentMissing == true {
		return ""
	}

	return "/fake-dev/disk/by-id/" + volumeID
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
