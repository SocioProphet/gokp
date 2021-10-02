package export

import (
	"context"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/scheme"
)

// ExportClusterYaml exports the given clusters YAML into the directory
func ExportClusterYaml(capicfg string, repodir string) (bool, error) {
	// Set up client to pass into the functions
	client, err := clientcmd.BuildConfigFromFlags("", capicfg)
	if err != nil {
		return false, err
	}

	// export cluster scoped YAML into the given directory
	// set and create the repodir for the cluser scoped things
	exportClusterDir := repodir + "/" + "core" + "/" + "cluster"
	err = os.MkdirAll(repodir, 0755)
	if err != nil {
		return false, err
	}

	_, err = exportClusterScopedYaml(client, exportClusterDir)
	if err != nil {
		return false, err
	}

	// If we're here we should be okay
	return true, nil
}

// exportClusterScopedYaml exports all the cluster scoped yaml into the given directory
func exportClusterScopedYaml(client *rest.Config, repodir string) (bool, error) {
	// Let's set up the client
	c, err := kubernetes.NewForConfig(client)
	if err != nil {
		return false, err
	}

	// First we get the NODES. Loop through each node
	nodes, _ := c.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	for _, node := range nodes.Items {
		// If we get nothing then don't bother, go to the next one
		if len(node.Name) == 0 {
			continue
		}

		// Get the config for a specific node
		nd, err := c.CoreV1().Nodes().Get(context.TODO(), node.Name, metav1.GetOptions{})
		if err != nil {
			panic(err)
		}
		//create the file
		y, err := os.Create(repodir + "/" + "node-" + strings.ReplaceAll(nd.Name, ":", "-") + ".yaml")
		if err != nil {
			panic(err)
		}

		// Make the YAML generic to store
		nd.SetResourceVersion("")
		nd.SetUID("")
		nd.SetAnnotations(map[string]string{})
		nd.CreationTimestamp.Reset()
		nd.SetSelfLink("")
		nd.SetManagedFields([]metav1.ManagedFieldsEntry{})
		nd.SetFinalizers([]string{})
		nd.SetOwnerReferences([]metav1.OwnerReference{})
		nd.SetGeneration(0)
		nd.Status.Reset()
		//needed for nodes maybe?
		nd.Status.DaemonEndpoints.Reset()
		nd.Status.NodeInfo.Reset()

		// take the node and write the file, first adding any
		// missing object information
		addTypeInformationToObject(nd)
		e := json.NewYAMLSerializer(json.DefaultMetaFactory, nil, nil)

		err = e.Encode(nd, y)
		if err != nil {
			panic(err.Error())
		}
		// close the file
		y.Close()

	}
	// - END NODES -

	// If we're here we should be okay
	return true, nil
}

// addTypeInformationToObject adds any missing fields to the runtime object
func addTypeInformationToObject(obj runtime.Object) error {
	gvks, _, err := scheme.Scheme.ObjectKinds(obj)
	if err != nil {
		return fmt.Errorf("missing apiVersion or kind and cannot assign it; %w", err)
	}

	for _, gvk := range gvks {
		if len(gvk.Kind) == 0 {
			continue
		}
		if len(gvk.Version) == 0 || gvk.Version == runtime.APIVersionInternal {
			continue
		}
		obj.GetObjectKind().SetGroupVersionKind(gvk)
		break
	}

	return nil
}
