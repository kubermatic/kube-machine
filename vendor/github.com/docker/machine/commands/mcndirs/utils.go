package mcndirs

import (
	"os"
	"path/filepath"

	"github.com/docker/machine/libmachine/mcnutils"
)

var (
	BaseDir = os.Getenv("KUBE_MACHINE_STORAGE_PATH")
)

func GetBaseDir() string {
	if BaseDir == "" {
		BaseDir = filepath.Join(mcnutils.GetHomeDir(), ".kube", "machine")
	}
	return BaseDir
}

func GetMachineDir() string {
	return filepath.Join(GetBaseDir(), "machines")
}

func GetMachineCertDir() string {
	return filepath.Join(GetBaseDir(), "certs")
}
