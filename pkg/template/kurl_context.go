package template

import (
	"reflect"
	"text/template"

	"github.com/pkg/errors"
	kurlclientset "github.com/replicatedhq/kurl/kurlkinds/client/kurlclientset/typed/cluster/v1beta1"
	kurlv1beta1 "github.com/replicatedhq/kurl/kurlkinds/pkg/apis/cluster/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func getKurlValues(installerName, nameSpace string) (*kurlv1beta1.Installer, error) {

	cfg, err := k8sconfig.GetConfig()

	if err != nil {
		return nil, errors.Wrap(err, "could not get config")
	}

	clientset := kurlclientset.NewForConfigOrDie(cfg)

	installers := clientset.Installers(nameSpace)

	retrieved, err := installers.Get(installerName, metav1.GetOptions{})

	if err != nil {
		return nil, errors.Wrap(err, "could not retrive installer crd object")
	}

	return retrieved, nil
}

func NewKurlContext(installerName, nameSpace string) (*KurlCtx, error) {
	kurlCtx := &KurlCtx{
		KurlValues: make(map[string]interface{}),
	}

	retrieved, err := getKurlValues(installerName, nameSpace)

	if err != nil {
		return nil, errors.Wrap(err, "could not retrieve kurl values")
	}

	v := reflect.ValueOf(retrieved.Spec.Kotsadm)

	typeOf := v.Type()

	for i := 0; i < v.NumField(); i++ {
		kurlCtx.KurlValues[typeOf.Field(i).Name] = v.Field(i).Interface()
	}

	return kurlCtx, nil
}

type KurlCtx struct {
	KurlValues map[string]interface{}
}

func (ctx KurlCtx) FuncMap() template.FuncMap {
	return template.FuncMap{
		"KurlMike": ctx.kurlMike,
		"KurlInt":  ctx.kurlInt,
	}
}

func (ctx KurlCtx) kurlInt(yamlPath string) int {
	// TODO: 1. check path exists
	// TODO: 2. check type is correct at path
	// TODO: 3. return what is at path

	return 1
}

func (ctx KurlCtx) kurlMike() string {
	result, ok := ctx.KurlValues["version"]

	if !ok {
		return "nope"
	}

	return result.(string)
}