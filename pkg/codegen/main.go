package main

import (
	"fmt"
	"os"

	v12 "github.com/cnrancher/ack-operator/pkg/apis/ack.pandaria.io/v1"

	_ "github.com/rancher/wrangler-api/pkg/generated/controllers/apiextensions.k8s.io"
	controllergen "github.com/rancher/wrangler/pkg/controller-gen"
	"github.com/rancher/wrangler/pkg/controller-gen/args"
	"github.com/rancher/wrangler/pkg/crd"
	"github.com/rancher/wrangler/pkg/yaml"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func main() {
	os.Unsetenv("GOPATH")

	controllergen.Run(args.Options{
		OutputPackage: "github.com/cnrancher/ack-operator/pkg/generated",
		Boilerplate:   "pkg/codegen/boilerplate.go.txt",
		Groups: map[string]args.Group{
			"ack.pandaria.io": {
				Types: []interface{}{
					"./pkg/apis/ack.pandaria.io/v1",
				},
				GenerateTypes: true,
			},
			// Optionally you can use wrangler-api project which
			// has a lot of common kubernetes APIs already generated.
			// In this controller we will use wrangler-api for apps api group
			"": {
				Types: []interface{}{
					v1.Pod{},
					v1.Node{},
					v1.Secret{},
				},
				InformersPackage: "k8s.io/client-go/informers",
				ClientSetPackage: "k8s.io/client-go/kubernetes",
				ListersPackage:   "k8s.io/client-go/listers",
			},
		},
	})

	ackClusterConfig := newCRD(&v12.ACKClusterConfig{}, func(c crd.CRD) crd.CRD {
		c.ShortNames = []string{"ackcc"}
		return c
	})

	obj, err := ackClusterConfig.ToCustomResourceDefinition()
	if err != nil {
		panic(err)
	}

	obj.ObjectMeta.Annotations = map[string]string{
		"helm.sh/resource-policy": "keep",
	}

	ackCCYaml, err := yaml.Export(&obj)
	if err != nil {
		panic(err)
	}

	if err := saveCRDYaml("ackclusterconfig", string(ackCCYaml)); err != nil {
		panic(err)
	}

	fmt.Printf("obj yaml: %s", ackCCYaml)
}

func newCRD(obj interface{}, customize func(crd.CRD) crd.CRD) crd.CRD {
	crd := crd.CRD{
		GVK: schema.GroupVersionKind{
			Group:   "ack.pandaria.io",
			Version: "v1",
		},
		Status:       true,
		SchemaObject: obj,
	}
	if customize != nil {
		crd = customize(crd)
	}
	return crd
}

func saveCRDYaml(name, yaml string) error {
	filename := fmt.Sprintf("./templates/%s.yaml", name)
	save, err := os.Create(filename)
	if err != nil {
		return err
	}

	defer save.Close()
	if err := save.Chmod(0755); err != nil {
		return err
	}

	if _, err := fmt.Fprint(save, yaml); err != nil {
		return err
	}

	return nil
}
