package nodestore

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// Only required to authenticate against GKE clusters
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/docker/machine/drivers/none"
	"github.com/docker/machine/libmachine/host"
)

var (
	defaultConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	kubeconfig    = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
)

type NodeStore struct {
	Path             string
	CaCertPath       string
	CaPrivateKeyPath string
	Client           kubernetes.Interface
}

func NewNodeStore(path, caCertPath, caPrivateKeyPath string) *NodeStore {
	var (
		err    error
		config *rest.Config
	)
	if _, err := os.Stat(defaultConfig); *kubeconfig == "" && os.IsNotExist(err) {
		config, err = rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}
	} else {
		if *kubeconfig == "" {
			*kubeconfig = defaultConfig
		}
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			panic(err.Error())
		}
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	return &NodeStore{
		Path:             path,
		CaCertPath:       caCertPath,
		CaPrivateKeyPath: caPrivateKeyPath,
		Client:           client,
	}
}

func (s NodeStore) GetMachinesDir() string {
	return filepath.Join(s.Path, "machines")
}

func (s NodeStore) saveToFile(data []byte, file string) error {
	if _, err := os.Stat(file); os.IsNotExist(err) {
		return ioutil.WriteFile(file, data, 0600)
	}

	tmpfi, err := ioutil.TempFile(filepath.Dir(file), "config.json.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmpfi.Name())

	if err = ioutil.WriteFile(tmpfi.Name(), data, 0600); err != nil {
		return err
	}

	if err = tmpfi.Close(); err != nil {
		return err
	}

	if err = os.Remove(file); err != nil {
		return err
	}

	if err = os.Rename(tmpfi.Name(), file); err != nil {
		return err
	}
	return nil
}

func (s NodeStore) Save(host *host.Host) error {
	data, err := json.MarshalIndent(host, "", "    ")
	if err != nil {
		return err
	}

	hostPath := filepath.Join(s.GetMachinesDir(), host.Name)

	// Ensure that the directory we want to save to exists.
	if err := os.MkdirAll(hostPath, 0700); err != nil {
		return err
	}

	return s.saveToFile(data, filepath.Join(hostPath, "config.json"))
}

func (s NodeStore) Remove(name string) error {
	return s.Client.CoreV1().Nodes().Delete(name, &metav1.DeleteOptions{})
}

func (s NodeStore) List() ([]string, error) {
	nodes, err := s.Client.CoreV1().Nodes().List(metav1.ListOptions{ /*LabelSelector: "kube-machine=true"*/ })
	if err != nil {
		return nil, err
	}

	hostNames := []string{}
	for i := range nodes.Items {
		node := &nodes.Items[i]
		hostNames = append(hostNames, node.Name)
	}

	return hostNames, nil
}

func (s NodeStore) Exists(name string) (bool, error) {
	_, err := s.Client.CoreV1().Nodes().Get(name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s NodeStore) loadConfig(h *host.Host) error {
	data, err := ioutil.ReadFile(filepath.Join(s.GetMachinesDir(), h.Name, "config.json"))
	if err != nil {
		return err
	}

	// Remember the machine name so we don't have to pass it through each
	// struct in the migration.
	name := h.Name

	migratedHost, migrationPerformed, err := host.MigrateHost(h, data)
	if err != nil {
		return fmt.Errorf("Error getting migrated host: %s", err)
	}

	*h = *migratedHost

	h.Name = name

	// If we end up performing a migration, we should save afterwards so we don't have to do it again on subsequent invocations.
	if migrationPerformed {
		if err := s.saveToFile(data, filepath.Join(s.GetMachinesDir(), h.Name, "config.json.bak")); err != nil {
			return fmt.Errorf("Error attempting to save backup after migration: %s", err)
		}

		if err := s.Save(h); err != nil {
			return fmt.Errorf("Error saving config after migration was performed: %s", err)
		}
	}

	return nil
}

func (s NodeStore) Load(name string) (*host.Host, error) {
	_, err := s.Client.CoreV1().Nodes().Get(name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &host.Host{
		Name:          name,
		ConfigVersion: 3,
		Driver:        none.NewDriver(name, "https://1.2.3.4:1234"),
		DriverName:    "none",
		HostOptions: &host.Options{
			Driver: "none",
			Memory: 42,
			Disk:   1234,
		},
		//RawDriver: []byte("{}"),
	}, nil
}
