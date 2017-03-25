package nodestore

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	kcorev1 "k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	// Only required to authenticate against GKE clusters
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/docker/machine/libmachine/host"
	"github.com/docker/machine/libmachine/mcnerror"
)

const (
	KubeMachineAnnotationKey = "node.alpha.kubernetes.io/kube-machine"
	KubeMachineLabel         = "kube-machine"
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

func (s NodeStore) Save(host *host.Host) error {
	data, err := json.MarshalIndent(host, "", "    ")
	if err != nil {
		return err
	}

	node, err := s.Client.CoreV1().Nodes().Get(host.Name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		node = &kcorev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: host.Name,
				Annotations: map[string]string{
					KubeMachineAnnotationKey: string(data),
				},
				Labels: map[string]string{
					KubeMachineLabel: "true",
				},
			},
		}
		_, err := s.Client.CoreV1().Nodes().Create(node)
		return err
	} else if err != nil {
		return err
	}

	if node.Annotations == nil {
		node.Annotations = map[string]string{}
	}
	node.Annotations[KubeMachineAnnotationKey] = string(data)

	if node.Labels == nil {
		node.Labels = map[string]string{}
	}
	node.Labels[KubeMachineLabel] = "true"

	_, err = s.Client.CoreV1().Nodes().Update(node)
	return err
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
		if _, exists := node.Annotations[KubeMachineAnnotationKey]; exists {
			hostNames = append(hostNames, node.Name)
		}
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

func (s NodeStore) loadConfig(node *kcorev1.Node, h *host.Host) error {
	data, exists := node.Annotations[KubeMachineAnnotationKey]
	if !exists {
		return os.ErrNotExist
	}

	// Remember the machine name so we don't have to pass it through each
	// struct in the migration.
	name := h.Name

	migratedHost, migrationPerformed, err := host.MigrateHost(h, []byte(data))
	if err != nil {
		return fmt.Errorf("Error getting migrated host: %s", err)
	}

	*h = *migratedHost

	h.Name = name

	// If we end up performing a migration, we should save afterwards so we don't have to do it again on subsequent invocations.
	if migrationPerformed {
		if err := s.Save(h); err != nil {
			return fmt.Errorf("Error saving config after migration was performed: %s", err)
		}
	}

	return nil
}

func (s NodeStore) Load(name string) (*host.Host, error) {
	node, err := s.Client.CoreV1().Nodes().Get(name, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		return nil, mcnerror.ErrHostDoesNotExist{
			Name: name,
		}
	}
	if err != nil {
		return nil, err
	}

	host := &host.Host{
		Name: name,
	}

	if err := s.loadConfig(node, host); err != nil {
		return nil, err
	}

	return host, nil
}
